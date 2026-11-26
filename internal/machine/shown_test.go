package machine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"connectrpc.com/connect"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

func TestShownPictures(t *testing.T) {
	got := shownPictures("see ![a](/tmp/a.png) and ![b](<shots/b c.png> \"title\") ![](https://example.com/x.png)\n" +
		"![d](data:image/png;base64,AAAA) ![e](file:///tmp/e.png) ![a again](/tmp/a.png) ![w](C:\\shots\\w.png) [link](/tmp/l.png)")
	want := []string{"/tmp/a.png", "shots/b c.png", "file:///tmp/e.png", `C:\shots\w.png`}
	if !slices.Equal(got, want) {
		t.Fatalf("shown: %q", got)
	}
	if p := pictureFile("shots/b.png", "/work"); p != filepath.Join("/work", "shots", "b.png") {
		t.Fatalf("relative: %s", p)
	}
	if p := pictureFile("file:///tmp/e%20f.png", "/work"); p != filepath.Clean("/tmp/e f.png") {
		t.Fatalf("file URL: %s", p)
	}
	if p := pictureFile("shots/b.png", ""); p != "" {
		t.Fatalf("relative without a directory: %s", p)
	}
}

func shaOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// 工单 #156/#158: a session gives back what a user message attached and the
// pictures its replies showed (kept when the turn ends), by SHA-256, and
// nothing else.
func TestReadAttachment(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	ctx := context.Background()
	pic := pngBytes(t)
	sess, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "fake", Prompt: "images",
		Attachments: []*sessionv1.Attachment{{Name: "shot.png", Data: pic}, {Name: "notes.txt", Data: []byte("notes")}}})
	if err != nil {
		t.Fatal(err)
	}
	id := sess.SessionId
	h.wait(id, idle)
	read := func(p *Principal, sha, path string) (*sessionv1.ReadAttachmentResponse, error) {
		return h.s.ReadAttachment(p, &sessionv1.ReadAttachmentRequest{SessionId: id, Sha256: sha, Path: path})
	}
	notFound := func(res *sessionv1.ReadAttachmentResponse, err error) {
		t.Helper()
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Fatalf("want not found, got %v %v", res.GetName(), err)
		}
	}

	// What a user message attached, any file.
	if res, err := read(h.p, shaOf(pic), ""); err != nil || !bytes.Equal(res.Data, pic) || res.Name != "shot.png" || res.MediaType != "image/png" {
		t.Fatalf("attached picture: %v %v", res.GetName(), err)
	}
	if res, err := read(h.p, shaOf([]byte("notes")), ""); err != nil || string(res.Data) != "notes" {
		t.Fatalf("attached notes: %v", err)
	}
	notFound(read(h.p, shaOf([]byte("never sent")), ""))
	if _, err := read(h.p, "../../etc/passwd", ""); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad sha: %v", err)
	}

	// The pictures a reply shows are kept when the turn ends: the same file once,
	// never a web address, never a file that is not a picture.
	dir := t.TempDir()
	shot := filepath.Join(dir, "screen.png")
	secret := filepath.Join(dir, "secret.txt")
	other := filepath.Join(dir, "other.png")
	os.WriteFile(shot, pic, 0o600)
	os.WriteFile(secret, []byte("the secret is 42"), 0o600)
	os.WriteFile(other, pic, 0o600)
	h.send(id, "say look ![screen]("+shot+") again ![same]("+shot+") ![s]("+secret+") ![w](https://example.com/x.png) ![later]("+filepath.Join(dir, "later.png")+")")
	h.wait(id, idle)
	tc := h.lastTurn(id)
	if len(tc.Shown) != 1 || tc.Shown[0].Path != shot || tc.Shown[0].Sha256 != shaOf(pic) || tc.Shown[0].Name != "screen.png" || !tc.Shown[0].Image {
		t.Fatalf("shown: %v", tc.Shown)
	}
	// Kept as the turn left it: changed or gone on disk, it is still there.
	os.WriteFile(shot, []byte("\x89PNG\r\n\x1a\nchanged"), 0o600)
	if res, err := read(h.p, tc.Shown[0].Sha256, ""); err != nil || !bytes.Equal(res.Data, pic) {
		t.Fatalf("kept picture: %v", err)
	}
	if res, err := read(h.p, "", shot); err != nil || !bytes.Equal(res.Data, pic) {
		t.Fatalf("kept picture by its path: %v", err)
	}

	// A picture a reply showed that no turn kept (here: made after the turn
	// ended): the owner only, as it is now.
	later := filepath.Join(dir, "later.png")
	os.WriteFile(later, pic, 0o600)
	notFound(read(h.p, "", later))
	owner := &Principal{Kind: PrincipalOwner}
	if res, err := read(owner, "", later); err != nil || !bytes.Equal(res.Data, pic) || res.Sha256 != shaOf(pic) {
		t.Fatalf("shown, not kept: %v", err)
	}
	// Never a file no reply showed, nor one that is not a picture.
	notFound(read(owner, "", other))
	notFound(read(owner, "", secret))

	// Another session's files are not this one's.
	sess2, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "fake", Prompt: "say hi"})
	if err != nil {
		t.Fatal(err)
	}
	h.wait(sess2.SessionId, idle)
	if _, err := h.s.ReadAttachment(h.p, &sessionv1.ReadAttachmentRequest{SessionId: sess2.SessionId, Sha256: shaOf(pic)}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("another session's file: %v", err)
	}
}
