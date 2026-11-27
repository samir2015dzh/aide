package provider

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/samir2015dzh/aide/internal/brandenv"
)

// Agent CLI updates (工单 #178), after paseo's release-check.ts and
// update-command.ts (https://github.com/getpaseo/paseo, Apache License 2.0):
// an agent whose CLI updates itself says the newest release its channel
// offers and the command that updates it; Aide compares the two versions,
// never saying "current" when it cannot tell, and runs that command, never
// replacing the program itself.

// Updater is an agent whose CLI updates itself.
type Updater interface {
	// LatestRelease is the newest version its release channel offers.
	LatestRelease(ctx context.Context) (string, error)
	// UpdateCommand is its CLI's own updater: bin and its arguments.
	UpdateCommand() (bin string, args []string)
}

// UpdateState is what a release channel says against the version here.
type UpdateState int

const (
	UpdateUnknown   UpdateState = iota // it cannot tell (Message says why)
	UpdateCurrent                      // the version here is the newest, or newer
	UpdateAvailable                    // a newer release is out
)

// UpdateCheck is CompareRelease's answer.
type UpdateCheck struct {
	State   UpdateState
	Message string
}

var releaseRE = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?`)

type release struct {
	text       string
	numbers    [3]int
	prerelease bool
}

func parseRelease(v string) (release, bool) {
	m := releaseRE.FindStringSubmatch(v)
	if m == nil {
		return release{}, false
	}
	var r release
	r.text, r.prerelease = m[0], m[4] != ""
	for i := range 3 {
		r.numbers[i], _ = strconv.Atoi(m[i+1])
	}
	return r, true
}

// CompareRelease compares major.minor.patch releases, the scheme Claude
// Code and Codex publish: a prerelease comes before the release with the
// same numbers, and two prereleases of one release do not compare.
func CompareRelease(installed, latest string) UpdateCheck {
	in, ok := parseRelease(installed)
	if !ok {
		return UpdateCheck{UpdateUnknown, "the installed version is not readable"}
	}
	out, ok := parseRelease(latest)
	if !ok {
		return UpdateCheck{UpdateUnknown, fmt.Sprintf("unrecognized release %q", latest)}
	}
	for i := range 3 {
		switch d := out.numbers[i] - in.numbers[i]; {
		case d > 0:
			return UpdateCheck{State: UpdateAvailable}
		case d < 0:
			return UpdateCheck{State: UpdateCurrent}
		}
	}
	switch {
	case out.prerelease && in.prerelease && out.text != in.text:
		return UpdateCheck{UpdateUnknown, fmt.Sprintf("cannot compare %s with %s", in.text, out.text)}
	case in.prerelease && !out.prerelease:
		return UpdateCheck{State: UpdateAvailable}
	}
	return UpdateCheck{State: UpdateCurrent}
}

// npmRegistry is where NPMLatest asks: AIDE_NPM_REGISTRY stands in for it
// (tests, or a mirror the machine is told to use).
var npmRegistry = cmp.Or(brandenv.Get("NPM_REGISTRY"), "https://registry.npmjs.org")

// NPMLatest is the version npm's registry tags latest for pkg.
func NPMLatest(ctx context.Context, pkg string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", npmRegistry+"/"+url.PathEscape(pkg)+"/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("npm registry: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry: %s for %s", res.Status, pkg)
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil || strings.TrimSpace(doc.Version) == "" {
		return "", errors.New("npm registry: no version for " + pkg)
	}
	return strings.TrimSpace(doc.Version), nil
}
