package contacts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestCardDAVSearch(t *testing.T) {
	const user, pass = "canary-user", "canary-icloud-password"
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if u, p, ok := r.BasicAuth(); !ok || u != user || p != pass {
			t.Error("expected synthetic credentials on iCloud request")
		}
		body, _ := io.ReadAll(r.Body)
		var property string
		href := r.URL.Path
		switch href {
		case "/":
			property = "<d:current-user-principal><d:href>/principal/</d:href></d:current-user-principal>"
		case "/principal/":
			property = "<c:addressbook-home-set><d:href>/dav/addressbooks/</d:href></c:addressbook-home-set>"
		case "/dav/addressbooks/":
			href = "/dav/addressbooks/book/"
			property = "<d:resourcetype><d:collection/><c:addressbook/></d:resourcetype>"
		case "/dav/addressbooks/book/":
			if !strings.Contains(string(body), "Mom &amp; Dad") || !strings.Contains(string(body), "addressbook-query") {
				t.Errorf("invalid query %q", body)
			}
			href = "/dav/addressbooks/book/id.vcf"
			property = "<c:address-data><![CDATA[BEGIN:VCARD\nVERSION:3.0\nFN:Jane Doe\nN:Doe;Jane;;;\nNICKNAME:Mom & Dad\nitem1.EMAIL;TYPE=INTERNET:jane@example.invalid\nTEL;VALUE=uri:tel:+15550123456\nNOTE:out-of-scope\nEND:VCARD\n]]></c:address-data>"
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprintf(w, `<d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:carddav"><d:response><d:href>%s</d:href><d:propstat><d:prop>%s</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, href, property)
	}))
	defer server.Close()

	c, err := newClient(server.URL, user, pass)
	if err != nil {
		t.Fatal(err)
	}
	matches, truncated, err := c.Search(t.Context(), "Mom & Dad")
	want := []Contact{{Name: "Jane Doe", Nicknames: []string{"Mom & Dad"}, Emails: []string{"jane@example.invalid"}, Phones: []string{"+15550123456"}}}
	if err != nil || truncated || !reflect.DeepEqual(matches, want) {
		t.Errorf("Search = %+v, %t, %v, want %+v", matches, truncated, err, want)
	}
	wantRequests := []string{"PROPFIND /", "PROPFIND /principal/", "PROPFIND /dav/addressbooks/", "REPORT /dav/addressbooks/book/"}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Errorf("requests = %v, want %v", requests, wantRequests)
	}
}

func TestOnlyICloudContactsEndpointsAreTrusted(t *testing.T) {
	c, err := NewClient("synthetic-user", "synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://contacts.icloud.com/", true},
		{"https://p12-contacts.icloud.com:443/", true},
		{"http://contacts.icloud.com/", false},
		{"https://p12-contacts.icloud.com.example.invalid/", false},
		{"https://www.icloud.com/", false},
		{"https://contacts.icloud.com:444/", false},
		{"https://name@contacts.icloud.com/", false},
	} {
		u, err := url.Parse(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.trusted(u); got != tc.ok {
			t.Errorf("trusted(%q) = %t, want %t", tc.url, got, tc.ok)
		}
	}
}

type fakeBackend struct{ searches int }

func (b *fakeBackend) Search(ctx context.Context, name string) ([]Contact, bool, error) {
	b.searches++
	return []Contact{{Name: name, Emails: []string{"mock@example.invalid"}}}, false, nil
}

func TestOnlyAuthorizedLoopbackCanCallReadOnlyTool(t *testing.T) {
	const token = "synthetic-bearer-canary-012345678901"
	backend := &fakeBackend{}
	handler, err := NewServer(backend, token)
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(auth, remote, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8794/mcp", strings.NewReader(body))
		r.RemoteAddr = remote
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), token) {
			t.Error("bearer token in response")
		}
		return w
	}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_contact","arguments":{"name":"Mock Contact"}}}`
	if w := invoke("", "127.0.0.1:65000", call); w.Code != http.StatusUnauthorized {
		t.Errorf("uncredentialed response = %d", w.Code)
	}
	auth := "Bearer " + token
	if w := invoke(auth, "192.0.2.100:65000", call); w.Code != http.StatusForbidden {
		t.Errorf("non-loopback response = %d", w.Code)
	}
	if w := invoke(auth, "127.0.0.1:65000", strings.Replace(call, "find_contact", "send_message", 1)); !strings.Contains(w.Body.String(), "-32602") {
		t.Errorf("send result = %s", w.Body)
	}
	list := invoke(auth, "127.0.0.1:65000", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !strings.Contains(list.Body.String(), `"readOnlyHint":true`) || !strings.Contains(list.Body.String(), "find_contact") {
		t.Errorf("tools/list = %s", list.Body)
	}
	match := invoke(auth, "127.0.0.1:65000", call)
	if !strings.Contains(match.Body.String(), "mock@example.invalid") || backend.searches != 1 {
		t.Errorf("lookup = %s, %d searches", match.Body, backend.searches)
	}
}

func TestCardDAVErrorsDoNotExposeCredentialsOrFollowForeignRedirects(t *testing.T) {
	const pass = "synthetic-app-password"
	for _, redirect := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if redirect {
				w.Header().Set("Location", "http://example.invalid/"+pass)
				w.WriteHeader(http.StatusTemporaryRedirect)
			} else {
				http.Error(w, pass, http.StatusUnauthorized)
			}
		}))
		c, err := newClient(server.URL, "synthetic-user", pass)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		_, _, err = c.Search(t.Context(), "Synthetic")
		server.Close()
		if err == nil || strings.Contains(err.Error(), pass) {
			t.Errorf("redirect = %t: expected a sanitized error, got %v", redirect, err)
		}
	}
}
