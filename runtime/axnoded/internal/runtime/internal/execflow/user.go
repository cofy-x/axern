package execflow

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

type passwdEntry struct {
	name  string
	uid   uint32
	gid   uint32
	home  string
	shell string
}

type groupEntry struct {
	name string
	gid  uint32
}

type ResolvedUser struct {
	OCI   spec.User
	Name  string
	Home  string
	Shell string
}

func ResolveUser(specConf *spec.Spec, user string) (spec.User, error) {
	resolved, err := ResolveUserInfo(specConf, user)
	if err != nil {
		return spec.User{}, err
	}
	return resolved.OCI, nil
}

func ResolveUserInfo(specConf *spec.Spec, user string) (ResolvedUser, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return ResolvedUser{}, fmt.Errorf("exec user can not be empty")
	}
	if !validUserSpec(user) {
		return ResolvedUser{}, fmt.Errorf("exec user %q is invalid", user)
	}

	userPart, groupPart, hasGroup := strings.Cut(user, ":")
	if userPart == "" || (hasGroup && groupPart == "") {
		return ResolvedUser{}, fmt.Errorf("exec user %q is invalid", user)
	}

	if uid, ok, err := parseUint32(userPart); err != nil {
		return ResolvedUser{}, fmt.Errorf("exec user %q has invalid uid: %w", userPart, err)
	} else if ok {
		out := ResolvedUser{OCI: spec.User{UID: uid}}
		if passwd, err := readPasswdOptional(rootfsPath(specConf)); err == nil {
			for _, entry := range passwd {
				if entry.uid == uid {
					out.OCI.GID = entry.gid
					out.Name = entry.name
					out.Home = entry.home
					out.Shell = entry.shell
					break
				}
			}
		}
		if hasGroup {
			if gid, groupOK, groupErr := parseUint32(groupPart); groupErr != nil {
				return ResolvedUser{}, fmt.Errorf("exec group %q has invalid gid: %w", groupPart, groupErr)
			} else if groupOK {
				out.OCI.GID = gid
				return out, nil
			}
		} else {
			return out, nil
		}
		groups, err := readGroup(rootfsPath(specConf))
		if err != nil {
			return ResolvedUser{}, err
		}
		gid, err := resolveGroupPart(groupPart, groups)
		if err != nil {
			return ResolvedUser{}, err
		}
		out.OCI.GID = gid
		return out, nil
	}

	rootfs := rootfsPath(specConf)
	passwd, err := readPasswd(rootfs)
	if err != nil {
		return ResolvedUser{}, err
	}

	resolved, ok := resolveNamedUser(userPart, passwd)
	if !ok {
		return ResolvedUser{}, fmt.Errorf("exec user %q was not found in container /etc/passwd", userPart)
	}

	if hasGroup {
		if gid, ok, err := parseUint32(groupPart); err != nil {
			return ResolvedUser{}, fmt.Errorf("exec group %q has invalid gid: %w", groupPart, err)
		} else if ok {
			resolved.OCI.GID = gid
			return resolved, nil
		}
		groups, err := readGroup(rootfs)
		if err != nil {
			return ResolvedUser{}, err
		}
		gid, err := resolveGroupPart(groupPart, groups)
		if err != nil {
			return ResolvedUser{}, err
		}
		resolved.OCI.GID = gid
	}
	return resolved, nil
}

func rootfsPath(specConf *spec.Spec) string {
	if specConf != nil && specConf.Root != nil {
		return specConf.Root.Path
	}
	return ""
}

func resolveNamedUser(user string, passwd []passwdEntry) (ResolvedUser, bool) {
	for _, entry := range passwd {
		if entry.name == user {
			return ResolvedUser{
				OCI: spec.User{
					UID:      entry.uid,
					GID:      entry.gid,
					Username: entry.name,
				},
				Name:  entry.name,
				Home:  entry.home,
				Shell: entry.shell,
			}, true
		}
	}
	return ResolvedUser{}, false
}

func resolveGroupPart(group string, groups []groupEntry) (uint32, error) {
	if gid, ok, err := parseUint32(group); err != nil {
		return 0, fmt.Errorf("exec group %q has invalid gid: %w", group, err)
	} else if ok {
		return gid, nil
	}
	for _, entry := range groups {
		if entry.name == group {
			return entry.gid, nil
		}
	}
	return 0, fmt.Errorf("exec group %q was not found in container /etc/group", group)
}

func readPasswd(rootfs string) ([]passwdEntry, error) {
	content, err := readRootfsFile(rootfs, "etc/passwd")
	if err != nil {
		return nil, err
	}
	return parsePasswd(content)
}

func readPasswdOptional(rootfs string) ([]passwdEntry, error) {
	if rootfs == "" {
		return nil, nil
	}
	content, err := os.ReadFile(filepath.Join(rootfs, "etc/passwd"))
	if err != nil {
		return nil, err
	}
	return parsePasswd(content)
}

func readGroup(rootfs string) ([]groupEntry, error) {
	content, err := readRootfsFile(rootfs, "etc/group")
	if err != nil {
		return nil, err
	}
	var out []groupEntry
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 3 {
			continue
		}
		gid, gidErr := strconv.ParseUint(fields[2], 10, 32)
		if gidErr != nil {
			continue
		}
		out = append(out, groupEntry{name: fields[0], gid: uint32(gid)})
	}
	return out, scanner.Err()
}

func readRootfsFile(rootfs string, name string) ([]byte, error) {
	if rootfs == "" {
		return nil, fmt.Errorf("container rootfs is required to resolve named exec users")
	}
	content, err := os.ReadFile(filepath.Join(rootfs, name))
	if err != nil {
		return nil, fmt.Errorf("read container /%s: %w", name, err)
	}
	return content, nil
}

func parsePasswd(content []byte) ([]passwdEntry, error) {
	var out []passwdEntry
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 4 {
			continue
		}
		uid, uidErr := strconv.ParseUint(fields[2], 10, 32)
		gid, gidErr := strconv.ParseUint(fields[3], 10, 32)
		if uidErr != nil || gidErr != nil {
			continue
		}
		entry := passwdEntry{name: fields[0], uid: uint32(uid), gid: uint32(gid)}
		if len(fields) > 5 {
			entry.home = fields[5]
		}
		if len(fields) > 6 {
			entry.shell = fields[6]
		}
		out = append(out, entry)
	}
	return out, scanner.Err()
}

func parseUint32(value string) (uint32, bool, error) {
	if value == "" {
		return 0, false, nil
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false, nil
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), true, err
}

func validUserSpec(user string) bool {
	colons := 0
	for _, r := range user {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		case r == ':':
			colons++
			if colons > 1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
