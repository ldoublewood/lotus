package stores

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"golang.org/x/xerrors"
)

func move(from, to string) error {
	from, err := homedir.Expand(from)
	if err != nil {
		return xerrors.Errorf("move: expanding from: %w", err)
	}

	to, err = homedir.Expand(to)
	if err != nil {
		return xerrors.Errorf("move: expanding to: %w", err)
	}

	if filepath.Base(from) != filepath.Base(to) {
		return xerrors.Errorf("move: base names must match ('%s' != '%s')", filepath.Base(from), filepath.Base(to))
	}

	log.Debugw("move sector data", "from", from, "to", to)

	toDir := filepath.Dir(to)

	// `mv` has decades of experience in moving files quickly; don't pretend we
	//  can do better

	var errOut bytes.Buffer
	cmd := exec.Command("/usr/bin/env", "mv", "-t", toDir, from) // nolint
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("exec mv (stderr: %s): %w", strings.TrimSpace(errOut.String()), err)
	}

	return nil
}


func moveAndLink(from, to string) error {
	from, err := homedir.Expand(from)
	if err != nil {
		return xerrors.Errorf("move: expanding from: %w", err)
	}

	to, err = homedir.Expand(to)
	if err != nil {
		return xerrors.Errorf("move: expanding to: %w", err)
	}

	if filepath.Base(from) != filepath.Base(to) {
		return xerrors.Errorf("move: base names must match ('%s' != '%s')", filepath.Base(from), filepath.Base(to))
	}

	log.Debugw("move sector data", "from", from, "to", to)

	toDir := filepath.Dir(to)

	// `mv` has decades of experience in moving files quickly; don't pretend we
	//  can do better

	var errOut bytes.Buffer
	cmd := exec.Command("/usr/bin/env", "mv", "-t", toDir, from)
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("exec mv (stderr: %s): %w", strings.TrimSpace(errOut.String()), err)
	}

	cmd = exec.Command("/usr/bin/env", "ln", "-s", to, from)
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("exec ln (stderr: %s): %w", strings.TrimSpace(errOut.String()), err)
	}

	return nil
}

func forceLink(from, to string) error {
	from, err := homedir.Expand(from)
	if err != nil {
		return xerrors.Errorf("move: expanding from: %w", err)
	}

	to, err = homedir.Expand(to)
	if err != nil {
		return xerrors.Errorf("move: expanding to: %w", err)
	}

	if filepath.Base(from) != filepath.Base(to) {
		return xerrors.Errorf("move: base names must match ('%s' != '%s')", filepath.Base(from), filepath.Base(to))
	}

	log.Debugw("force link", "from", from, "to", to)
	err = os.RemoveAll(to)
	if err != nil {
		log.Errorf("removing dest dir %s: %+v", to, err)
	}

	var errOut bytes.Buffer

	cmd := exec.Command("/usr/bin/env", "ln", "-sf", from, to)
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("exec ln -sf (stderr: %s): %w", strings.TrimSpace(errOut.String()), err)
	}

	return nil
}

func GetChildren(parent string) ([]string, []string, error) {
	var nonlinks, links []string
	files, err := ioutil.ReadDir(parent)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		if f.Mode()&os.ModeSymlink != 0 {
			// symlink
			links = append(links, f.Name())
		} else {
			nonlinks  = append(nonlinks, f.Name())
		}
	}
	return nonlinks, links, nil
}
