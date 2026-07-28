package process

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type processUser struct {
	uid    uint32
	gid    uint32
	groups []uint32
	name   string
	home   string
	shell  string
}

type passwdEntry struct {
	name  string
	uid   uint32
	gid   uint32
	home  string
	shell string
}

type groupEntry struct {
	name    string
	gid     uint32
	members []string
}

func resolveProcessUser(spec string) (processUser, bool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return processUser{}, false, nil
	}
	userPart, groupPart, hasGroup := strings.Cut(spec, ":")
	if userPart == "" || (hasGroup && groupPart == "") {
		return processUser{}, false, fmt.Errorf("process user %q is invalid", spec)
	}

	passwd, passwdErr := readContainerPasswd()
	groups, groupErr := readContainerGroups()
	if uid, ok, err := parseUserID(userPart); err != nil {
		return processUser{}, false, fmt.Errorf("process user %q has invalid uid: %w", userPart, err)
	} else if ok {
		resolved := processUser{
			uid:  uid,
			name: strconv.FormatUint(uint64(uid), 10),
			home: "/",
		}
		if passwdErr == nil {
			if entry, ok := findPasswdByUID(passwd, uid); ok {
				resolved.gid = entry.gid
				resolved.name = entry.name
				resolved.home = entry.home
				resolved.shell = entry.shell
			}
		}
		if hasGroup {
			gid, err := resolveGroupID(groupPart, groups, groupErr)
			if err != nil {
				return processUser{}, false, err
			}
			resolved.gid = gid
		}
		if groupErr == nil {
			resolved.groups = supplementaryGroups(resolved.name, groups)
		}
		return resolved, true, nil
	}

	if passwdErr != nil {
		return processUser{}, false, passwdErr
	}
	entry, ok := findPasswdByName(passwd, userPart)
	if !ok {
		return processUser{}, false, fmt.Errorf("process user %q was not found in /etc/passwd", userPart)
	}
	resolved := processUser{
		uid:   entry.uid,
		gid:   entry.gid,
		name:  entry.name,
		home:  entry.home,
		shell: entry.shell,
	}
	if hasGroup {
		gid, err := resolveGroupID(groupPart, groups, groupErr)
		if err != nil {
			return processUser{}, false, err
		}
		resolved.gid = gid
	}
	if groupErr == nil {
		resolved.groups = supplementaryGroups(resolved.name, groups)
	}
	return resolved, true, nil
}

func (u processUser) env() []string {
	out := make([]string, 0, 4)
	home := u.defaultCwd()
	if home != "" {
		out = append(out, "HOME="+home)
	}
	if u.name != "" {
		out = append(out, "USER="+u.name, "LOGNAME="+u.name)
	}
	if u.shell != "" {
		out = append(out, "SHELL="+u.shell)
	}
	return out
}

func (u processUser) defaultCwd() string {
	if home := strings.TrimSpace(u.home); home != "" {
		return home
	}
	if u.name != "" || u.uid != 0 || u.gid != 0 {
		return "/"
	}
	return ""
}

func readContainerPasswd() ([]passwdEntry, error) {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil, fmt.Errorf("read container /etc/passwd: %w", err)
	}
	return parsePasswd(data), nil
}

func readContainerGroups() ([]groupEntry, error) {
	data, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil, fmt.Errorf("read container /etc/group: %w", err)
	}
	return parseGroups(data), nil
}

func parsePasswd(data []byte) []passwdEntry {
	var out []passwdEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 7 {
			continue
		}
		uid, uidErr := strconv.ParseUint(fields[2], 10, 32)
		gid, gidErr := strconv.ParseUint(fields[3], 10, 32)
		if uidErr != nil || gidErr != nil {
			continue
		}
		out = append(out, passwdEntry{
			name:  fields[0],
			uid:   uint32(uid),
			gid:   uint32(gid),
			home:  fields[5],
			shell: fields[6],
		})
	}
	return out
}

func parseGroups(data []byte) []groupEntry {
	var out []groupEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 3 {
			continue
		}
		gid, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			continue
		}
		var members []string
		if len(fields) > 3 && fields[3] != "" {
			members = strings.Split(fields[3], ",")
		}
		out = append(out, groupEntry{name: fields[0], gid: uint32(gid), members: members})
	}
	return out
}

func parseUserID(raw string) (uint32, bool, error) {
	value, err := strconv.ParseUint(raw, 10, 32)
	if err == nil {
		return uint32(value), true, nil
	}
	if strings.ContainsAny(raw, "0123456789") && raw[0] >= '0' && raw[0] <= '9' {
		return 0, false, err
	}
	return 0, false, nil
}

func resolveGroupID(raw string, groups []groupEntry, groupErr error) (uint32, error) {
	if gid, ok, err := parseUserID(raw); err != nil {
		return 0, fmt.Errorf("process group %q has invalid gid: %w", raw, err)
	} else if ok {
		return gid, nil
	}
	if groupErr != nil {
		return 0, groupErr
	}
	for _, entry := range groups {
		if entry.name == raw {
			return entry.gid, nil
		}
	}
	return 0, fmt.Errorf("process group %q was not found in /etc/group", raw)
}

func findPasswdByUID(entries []passwdEntry, uid uint32) (passwdEntry, bool) {
	for _, entry := range entries {
		if entry.uid == uid {
			return entry, true
		}
	}
	return passwdEntry{}, false
}

func findPasswdByName(entries []passwdEntry, name string) (passwdEntry, bool) {
	for _, entry := range entries {
		if entry.name == name {
			return entry, true
		}
	}
	return passwdEntry{}, false
}

func supplementaryGroups(name string, groups []groupEntry) []uint32 {
	if name == "" {
		return nil
	}
	var out []uint32
	for _, group := range groups {
		for _, member := range group.members {
			if member == name {
				out = append(out, group.gid)
				break
			}
		}
	}
	return out
}
