package machine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"connectrpc.com/connect"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// Files a session keeps, given back to any device (工单 #156): what a user
// message attached, and the pictures the agent's replies showed, kept when
// their turn ends (工单 #158, after hermesh's turn-end pinning). Both are
// known by their SHA-256, so the same picture is one, however often shown.

const (
	// A picture a reply shows is kept up to this size; past it, only its
	// path stays in the reply.
	maxShownPicture = 10 << 20
	// At most this many pictures are kept of one turn.
	maxShownPerTurn = 16
)

// imageRef is a Markdown image: ![alt](path "title"), the path maybe in <>.
var imageRef = regexp.MustCompile(`!\[[^\]\n]*\]\(\s*(<[^>\n]+>|[^)\s]+)(?:\s+"[^"\n]*")?\s*\)`)

// shownPictures are the paths of the files a reply shows as images, in the
// order it shows them, each once: never a web address or inline data,
// which no device loads.
func shownPictures(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range imageRef.FindAllStringSubmatch(text, -1) {
		p := strings.TrimSuffix(strings.TrimPrefix(m[1], "<"), ">")
		if p == "" || seen[p] {
			continue
		}
		if i := strings.Index(p, ":"); i > 1 && !strings.HasPrefix(strings.ToLower(p), "file:") {
			continue // http:, https:, data: and the like (C:\ is a drive)
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// pictureFile is the file a reply's path names: file: URLs as their path,
// a relative path in the session's working directory.
func pictureFile(written, workDir string) string {
	p := written
	if strings.HasPrefix(strings.ToLower(p), "file:") {
		if u, err := url.Parse(p); err == nil && u.Path != "" {
			p = u.Path
			if len(p) > 2 && p[0] == '/' && p[2] == ':' { // file:///C:/x
				p = p[1:]
			}
		}
	} else if q, err := url.PathUnescape(p); err == nil {
		p = q
	}
	if !filepath.IsAbs(p) {
		if workDir == "" {
			return ""
		}
		p = filepath.Join(workDir, p)
	}
	return filepath.Clean(p)
}

// pictureType is a picture's media type by its content, or "" for anything
// that is not a PNG, JPEG, GIF or WebP.
func pictureType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// readPicture reads the picture at path: a regular file (links followed)
// of at most maxShownPicture whose content is a picture. ok is false for
// anything else, without saying why: a path that is not a picture tells
// nothing about the file.
func readPicture(path string) (data []byte, mediaType string, ok bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", false
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, "", false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > maxShownPicture {
		return nil, "", false
	}
	data, err = io.ReadAll(io.LimitReader(f, maxShownPicture+1))
	if err != nil || len(data) > maxShownPicture {
		return nil, "", false
	}
	if mediaType = pictureType(data); mediaType == "" {
		return nil, "", false
	}
	return data, mediaType, true
}

func (s *Sessions) shownDir(id string) string {
	return filepath.Join(s.attachmentsDir(id), "shown")
}

// keepShown keeps the pictures a turn's replies showed (their paths as
// written) with the session, by their SHA-256, and says what it kept: the
// same picture once; a path that names no picture is left out.
func (s *Sessions) keepShown(id, workDir string, written []string) []*sessionv1.AttachmentInfo {
	var out []*sessionv1.AttachmentInfo
	have := map[string]bool{}
	for _, w := range written {
		if len(out) == maxShownPerTurn {
			break
		}
		path := pictureFile(w, workDir)
		if path == "" {
			continue
		}
		data, mt, ok := readPicture(path)
		if !ok {
			continue
		}
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		if !have[sha] {
			dst := filepath.Join(s.shownDir(id), sha)
			if _, err := os.Stat(dst); errors.Is(err, fs.ErrNotExist) {
				if err := writeKept(dst, data); err != nil {
					s.log.Warn("shown picture not kept", "session", id, "err", err)
					continue
				}
			}
		}
		have[sha] = true
		out = append(out, &sessionv1.AttachmentInfo{Name: filepath.Base(path), MediaType: mt, Size: uint64(len(data)), Path: w, Sha256: sha, Image: true})
	}
	return out
}

// writeKept writes a kept file whole or not at all.
func writeKept(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ReadAttachment gives back a file the session keeps
// (SessionService.ReadAttachment).
func (s *Sessions) ReadAttachment(p *Principal, req *sessionv1.ReadAttachmentRequest) (*sessionv1.ReadAttachmentResponse, error) {
	_, r, err := s.find(p, req.SessionId, false)
	if err != nil {
		return nil, err
	}
	sha := strings.ToLower(req.Sha256)
	if sha == "" && req.Path == "" || sha != "" && !isSHA256(sha) {
		return nil, cerr(connect.CodeInvalidArgument, "a file is asked for by its sha256 or a picture by its path")
	}
	var found *sessionv1.AttachmentInfo // the file, as recorded
	kept := false                       // in shownDir
	shown := false                      // a reply showed req.Path
	for after := uint64(0); found == nil; {
		evs, err := s.st.Events(req.SessionId, after, 500)
		if err != nil {
			return nil, err
		}
		if len(evs) == 0 {
			break
		}
		for _, e := range evs {
			switch k := e.Kind.(type) {
			case *sessionv1.SessionEvent_UserMessage:
				for _, f := range k.UserMessage.Attachments {
					if sha != "" && f.Sha256 == sha && found == nil {
						found = f
					}
				}
			case *sessionv1.SessionEvent_TurnCompleted:
				for _, f := range k.TurnCompleted.Shown {
					if (sha != "" && f.Sha256 == sha || sha == "" && samePath(f.Path, req.Path)) && (found == nil || !kept) {
						found, kept = f, true
					}
				}
			case *sessionv1.SessionEvent_AgentMessage:
				if sha == "" && !shown && strings.Contains(k.AgentMessage.Text, "![") {
					shown = slices.ContainsFunc(shownPictures(k.AgentMessage.Text), func(w string) bool { return samePath(w, req.Path) })
				}
			}
		}
		after = evs[len(evs)-1].Seq
	}
	switch {
	case found != nil && kept:
		return readKept(filepath.Join(s.shownDir(req.SessionId), found.Sha256), found)
	case found != nil:
		return readKept(found.Path, found)
	case sha == "" && shown && p.Kind == PrincipalOwner:
		// Not kept yet (its turn runs) or never (a turn from before): the
		// picture as it is now.
		path := pictureFile(req.Path, r.Worktree)
		if data, mt, ok := readPicture(path); ok {
			sum := sha256.Sum256(data)
			return &sessionv1.ReadAttachmentResponse{Name: filepath.Base(path), MediaType: mt, Size: uint64(len(data)), Sha256: hex.EncodeToString(sum[:]), Data: data}, nil
		}
	}
	return nil, cerr(connect.CodeNotFound, "session %s keeps no such file", req.SessionId)
}

// readKept reads a kept file, which must still be what was recorded.
func readKept(path string, f *sessionv1.AttachmentInfo) (*sessionv1.ReadAttachmentResponse, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, cerr(connect.CodeNotFound, "%s is no longer kept on this machine", f.Name)
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != f.Sha256 {
		return nil, cerr(connect.CodeNotFound, "%s has changed on this machine since it was kept", f.Name)
	}
	return &sessionv1.ReadAttachmentResponse{Name: f.Name, MediaType: f.MediaType, Size: uint64(len(data)), Sha256: f.Sha256, Data: data}, nil
}

// samePath: two ways of writing one path, as written and as a Markdown
// renderer gives it (its URL encoding: spaces, non-ASCII names).
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ua, err1 := url.PathUnescape(a)
	ub, err2 := url.PathUnescape(b)
	return err1 == nil && err2 == nil && ua == ub
}

func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
