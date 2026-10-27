package updater

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// SigNamespace is the ssh-keygen -n namespace releases are signed under, so a
// signature made with the release key for anything else never verifies here
const SigNamespace = "bud-release"

// ReleaseKey is the pinned public half of the release key (D41), as an
// authorized_keys line. Keep it in step with RELEASE_KEY in install.sh
var ReleaseKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJsl2QmIh9DiX0baf+L0CgLKfFLQRf9GNRIgXFZK/hqY"

// ErrNoReleaseKey means this build has no pinned release key yet
var ErrNoReleaseKey = errors.New("this build has no release key pinned, so it can't verify downloads")

// VerifyRelease checks a release's checksums.txt against its signature and the
// pinned release key
func VerifyRelease(checksums, sig []byte) error {
	if ReleaseKey == "" {
		return ErrNoReleaseKey
	}

	return VerifySSHSig(ReleaseKey, SigNamespace, checksums, sig)
}

const sshsigMagic = "SSHSIG"

// VerifySSHSig checks an armored `ssh-keygen -Y sign` signature over message,
// made by the ed25519 key in pubKey (an authorized_keys line) under namespace.
// The format is OpenSSH's PROTOCOL.sshsig
func VerifySSHSig(pubKey, namespace string, message, armored []byte) error {
	want, err := parseEd25519PublicKey(pubKey)
	if err != nil {
		return err
	}

	block, _ := pem.Decode(armored)
	if block == nil || block.Type != "SSH SIGNATURE" {
		return errors.New("signature: not an armored SSH signature")
	}
	blob := block.Bytes
	if !bytes.HasPrefix(blob, []byte(sshsigMagic)) {
		return errors.New("signature: not an SSHSIG blob")
	}
	r := &wireReader{b: blob[len(sshsigMagic):]}
	ver := r.uint32()
	pub := r.string()
	ns := r.string()
	reserved := r.string()
	hashAlg := r.string()
	sigBlob := r.string()
	if r.err != nil || len(r.b) != 0 {
		return errors.New("signature: malformed SSHSIG blob")
	}
	if ver != 1 {
		return fmt.Errorf("signature: unsupported SSHSIG version %d", ver)
	}

	key, err := parseEd25519Wire(pub)
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	if !key.Equal(want) {
		return errors.New("signature: made by a different key")
	}
	if string(ns) != namespace {
		return fmt.Errorf("signature: namespace is %q, want %q", ns, namespace)
	}

	var digest []byte
	switch string(hashAlg) {
	case "sha512":
		h := sha512.Sum512(message)
		digest = h[:]
	case "sha256":
		h := sha256.Sum256(message)
		digest = h[:]
	default:
		return fmt.Errorf("signature: unsupported hash %q", hashAlg)
	}

	sr := &wireReader{b: sigBlob}
	sigType := sr.string()
	sig := sr.string()
	if sr.err != nil || len(sr.b) != 0 {
		return errors.New("signature: malformed signature")
	}
	if string(sigType) != "ssh-ed25519" || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature: unsupported signature type %q", sigType)
	}

	// What was actually signed: the magic, then namespace, reserved, hash
	// algorithm and the message's hash as SSH strings
	var signed bytes.Buffer
	signed.WriteString(sshsigMagic)
	for _, s := range [][]byte{ns, reserved, hashAlg, digest} {
		writeString(&signed, s)
	}
	if !ed25519.Verify(key, signed.Bytes(), sig) {
		return errors.New("signature: doesn't match (the file or signature was changed)")
	}

	return nil
}

// parseEd25519PublicKey reads an authorized_keys style "ssh-ed25519 AAAA... comment"
func parseEd25519PublicKey(line string) (ed25519.PublicKey, error) {
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "ssh-ed25519" {
		return nil, errors.New("public key: not an ssh-ed25519 key")
	}
	wire, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	k, err := parseEd25519Wire(wire)
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}

	return k, nil
}

// parseEd25519Wire reads a public key in SSH wire format
func parseEd25519Wire(wire []byte) (ed25519.PublicKey, error) {
	r := &wireReader{b: wire}
	typ := r.string()
	if r.err == nil && string(typ) != "ssh-ed25519" {
		return nil, fmt.Errorf("key type %q isn't ssh-ed25519", typ)
	}
	key := r.string()
	if r.err != nil || len(r.b) != 0 || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("malformed key")
	}

	return ed25519.PublicKey(key), nil
}

// wireReader reads SSH wire format (RFC 4251): uint32s and length-prefixed
// strings. The first short read sets err and every later read returns zero
type wireReader struct {
	b   []byte
	err error
}

func (r *wireReader) uint32() uint32 {
	if r.err != nil || len(r.b) < 4 {
		r.err = errors.New("short read")
		return 0
	}
	v := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]

	return v
}

func (r *wireReader) string() []byte {
	n := r.uint32()
	if r.err != nil || uint64(len(r.b)) < uint64(n) {
		r.err = errors.New("short read")
		return nil
	}
	s := r.b[:n]
	r.b = r.b[n:]

	return s
}

func writeString(b *bytes.Buffer, s []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(s)))
	b.Write(n[:])
	b.Write(s)
}
