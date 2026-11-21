// Package contacts implements Bud's read-only iCloud CardDAV lookup.
package contacts

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	addressBookNamespace = "urn:ietf:params:xml:ns:carddav"
	maxResponse          = 4 << 20
	maxBooks             = 32
	maxResults           = 10
)

var iCloudSibling = regexp.MustCompile(`^p[0-9]+-contacts\.icloud\.com$`)

// Client reads address books using only PROPFIND and REPORT.
type Client struct {
	base *url.URL
	user string
	pass string
	http *http.Client
}

func NewClient(user, pass string) (*Client, error) {
	return newClient("https://contacts.icloud.com/", user, pass)
}

func newClient(base, user, pass string) (*Client, error) {
	if strings.TrimSpace(user) == "" || strings.TrimSpace(pass) == "" {
		return nil, errors.New("set BUD_CARDDAV_USER and BUD_CARDDAV_PASSWORD through op run")
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("invalid CardDAV endpoint")
	}
	c := &Client{base: u, user: user, pass: pass}
	c.http = &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || req.Method != via[0].Method || !c.trusted(req.URL) {
				return errors.New("unsafe CardDAV redirect")
			}
			req.SetBasicAuth(c.user, c.pass)
			return nil
		},
	}
	return c, nil
}

func (c *Client) trusted(u *url.URL) bool {
	if u.User != nil || u.Scheme != c.base.Scheme {
		return false
	}
	if c.base.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		return ip != nil && ip.IsLoopback() && u.Host == c.base.Host
	}
	return u.Scheme == "https" && (u.Port() == "" || u.Port() == "443") &&
		(strings.EqualFold(u.Hostname(), c.base.Hostname()) || iCloudSibling.MatchString(strings.ToLower(u.Hostname())))
}

type multiStatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href   string    `xml:"DAV: href"`
	Status string    `xml:"DAV: status"`
	Props  []propSet `xml:"DAV: propstat"`
}

type hrefProp struct {
	Href string `xml:"DAV: href"`
}

type propSet struct {
	Status string `xml:"DAV: status"`
	Prop   struct {
		Principal hrefProp `xml:"DAV: current-user-principal"`
		Home      hrefProp `xml:"urn:ietf:params:xml:ns:carddav addressbook-home-set"`
		Resource  struct {
			Book *struct{} `xml:"urn:ietf:params:xml:ns:carddav addressbook"`
		} `xml:"DAV: resourcetype"`
		Card string `xml:"urn:ietf:params:xml:ns:carddav address-data"`
	} `xml:"DAV: prop"`
}

func (c *Client) request(ctx context.Context, method string, target *url.URL, depth, body string) (multiStatus, *url.URL, error) {
	var result multiStatus
	if !c.trusted(target) {
		return result, nil, errors.New("CardDAV discovery returned an untrusted endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), strings.NewReader(body))
	if err != nil {
		return result, nil, errors.New("could not prepare CardDAV request")
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.SetBasicAuth(c.user, c.pass)
	resp, err := c.http.Do(req)
	if err != nil {
		return result, nil, errors.New("CardDAV request failed; check connectivity and the iCloud endpoint")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return result, nil, errors.New("iCloud CardDAV refused access; check the account and app-specific password in 1Password")
	case http.StatusMultiStatus:
	default:
		return result, nil, fmt.Errorf("iCloud CardDAV returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return result, nil, errors.New("CardDAV response was unreadable or too large; try a more specific name")
	}
	if err := xml.Unmarshal(data, &result); err != nil {
		return result, nil, errors.New("CardDAV returned invalid WebDAV XML")
	}
	return result, resp.Request.URL, nil
}

func success(status string) bool {
	parts := strings.Fields(status)
	return len(parts) >= 2 && len(parts[1]) == 3 && parts[1][0] == '2'
}

func (c *Client) resolve(at *url.URL, raw string) (*url.URL, error) {
	href, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, errors.New("CardDAV discovery returned an invalid URL")
	}
	u := at.ResolveReference(href)
	if !c.trusted(u) || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("CardDAV discovery returned an untrusted endpoint")
	}
	return u, nil
}

func propfind(properties string) string {
	return `<d:propfind xmlns:d="DAV:" xmlns:c="` + addressBookNamespace + `"><d:prop>` + properties + `</d:prop></d:propfind>`
}

func (c *Client) discoveryURL(ctx context.Context, at *url.URL, property string, get func(propSet) string) (*url.URL, error) {
	result, from, err := c.request(ctx, "PROPFIND", at, "0", propfind(property))
	if err != nil {
		return nil, err
	}
	for _, response := range result.Responses {
		for _, prop := range response.Props {
			if value := get(prop); success(prop.Status) && strings.TrimSpace(value) != "" {
				return c.resolve(from, value)
			}
		}
	}
	return nil, errors.New("iCloud did not advertise a CardDAV principal or address book home")
}

func (c *Client) books(ctx context.Context) ([]*url.URL, error) {
	principal, err := c.discoveryURL(ctx, c.base, "<d:current-user-principal/>", func(p propSet) string { return p.Prop.Principal.Href })
	if err != nil {
		return nil, err
	}
	home, err := c.discoveryURL(ctx, principal, "<c:addressbook-home-set/>", func(p propSet) string { return p.Prop.Home.Href })
	if err != nil {
		return nil, err
	}
	result, from, err := c.request(ctx, "PROPFIND", home, "1", propfind("<d:resourcetype/>"))
	if err != nil {
		return nil, err
	}
	var books []*url.URL
	seen := map[string]bool{}
	for _, response := range result.Responses {
		for _, prop := range response.Props {
			if !success(prop.Status) || prop.Prop.Resource.Book == nil {
				continue
			}
			book, err := c.resolve(from, response.Href)
			if err != nil {
				return nil, err
			}
			if !seen[book.String()] {
				books = append(books, book)
				seen[book.String()] = true
				if len(books) > maxBooks {
					return nil, errors.New("iCloud returned too many address books")
				}
			}
		}
	}
	return books, nil
}

func nameReport(name string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(name))
	query := `<c:addressbook-query xmlns:d="DAV:" xmlns:c="` + addressBookNamespace + `"><d:prop><c:address-data/></d:prop><c:filter test="anyof">`
	for _, property := range []string{"FN", "N", "NICKNAME"} {
		query += `<c:prop-filter name="` + property + `"><c:text-match collation="i;unicode-casemap" match-type="contains">` + escaped.String() + `</c:text-match></c:prop-filter>`
	}
	return query + `</c:filter></c:addressbook-query>`
}

// Search returns up to ten matching cards and whether more matched. It
// never returns raw vCards or phone numbers and addresses from nonmatches.
func (c *Client) Search(ctx context.Context, name string) ([]Contact, bool, error) {
	books, err := c.books(ctx)
	if err != nil {
		return nil, false, err
	}
	contacts := []Contact{}
	query := nameReport(name)
	seen := map[string]bool{}
	for _, book := range books {
		result, _, err := c.request(ctx, "REPORT", book, "1", query)
		if err != nil {
			return nil, false, err
		}
		for _, response := range result.Responses {
			if response.Status != "" && !success(response.Status) {
				return nil, false, errors.New("iCloud could not complete the contact query; try a more specific name")
			}
			for _, prop := range response.Props {
				if !success(prop.Status) || prop.Prop.Card == "" {
					continue
				}
				contact, matches := parseCard(prop.Prop.Card, name)
				if !matches || seen[prop.Prop.Card] {
					continue
				}
				if len(contacts) == maxResults {
					return contacts, true, nil
				}
				seen[prop.Prop.Card] = true
				contacts = append(contacts, contact)
			}
		}
	}
	return contacts, false, nil
}
