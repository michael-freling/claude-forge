package claudecode

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/michael-freling/claude-forge/internal/forge/layout"
)

const (
	// ContainerPasswdFile and ContainerGroupFile are the names of the
	// forge-generated account databases inside the config directory
	// (~/.config/claude-forge/). They are mounted read-only over /etc/passwd
	// and /etc/group in the agent container.
	ContainerPasswdFile = "container-passwd"
	ContainerGroupFile  = "container-group"

	// ContainerUserName is the account name the host UID/GID resolve to in the
	// agent container.
	ContainerUserName = "user"
)

// ContainerIdentity holds the host paths of the generated account databases.
type ContainerIdentity struct {
	PasswdFile string
	GroupFile  string
}

// RenderContainerPasswd returns an /etc/passwd that maps uid:gid to the
// container user with home layout.Home.
//
// The agent container runs as the host user's UID:GID so files it writes to
// bind mounts keep the host owner, but that UID has no entry in the image's
// /etc/passwd. Tools that look up the current user (ssh, git, whoami) fail
// without one, and a writable /etc/passwd would let the agent add a root
// account for su, so the entry comes from this read-only mounted file instead.
func RenderContainerPasswd(uid, gid int) string {
	return fmt.Sprintf(
		"root:x:0:0:root:/root:/bin/bash\n"+
			"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n"+
			"%s:x:%d:%d::%s:/bin/bash\n",
		ContainerUserName, uid, gid, layout.Home,
	)
}

// RenderContainerGroup returns the /etc/group that pairs with
// RenderContainerPasswd.
func RenderContainerGroup(gid int) string {
	return fmt.Sprintf(
		"root:x:0:\n"+
			"nogroup:x:65534:\n"+
			"%s:x:%d:\n",
		ContainerUserName, gid,
	)
}

// WriteContainerIdentity writes the passwd and group files for uid:gid into
// configDir and returns their paths.
func WriteContainerIdentity(configDir string, uid, gid int) (ContainerIdentity, error) {
	if uid <= 0 || gid <= 0 {
		return ContainerIdentity{}, fmt.Errorf("container identity needs a non-root uid and gid, got %d:%d", uid, gid)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return ContainerIdentity{}, fmt.Errorf("failed to create config directory: %w", err)
	}

	id := ContainerIdentity{
		PasswdFile: filepath.Join(configDir, ContainerPasswdFile),
		GroupFile:  filepath.Join(configDir, ContainerGroupFile),
	}
	if err := writeFileAtomic(id.PasswdFile, []byte(RenderContainerPasswd(uid, gid))); err != nil {
		return ContainerIdentity{}, fmt.Errorf("failed to write %s: %w", ContainerPasswdFile, err)
	}
	if err := writeFileAtomic(id.GroupFile, []byte(RenderContainerGroup(gid))); err != nil {
		return ContainerIdentity{}, fmt.Errorf("failed to write %s: %w", ContainerGroupFile, err)
	}
	return id, nil
}

// writeFileAtomic writes data to path with mode 0644 so that a reader, such as
// a container bind-mounting the file while another session starts, never sees
// it truncated or half written. The file is left untouched when it already
// holds data; otherwise data goes to a temporary file in the same directory
// that is then renamed over path.
func writeFileAtomic(path string, data []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // fails harmlessly once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
