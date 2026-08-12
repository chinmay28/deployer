package deploy

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/chinmay28/deployer/server/internal/store"
)

// Nothing ever tells Deployer which ports an app serves on, but an app that
// has been set up properly says so twice over: an HTTP health check names the
// port that answers, and install commands take the port as a parameter. Those
// two are enough to show "on nakedpi · port 8899" beside an app, which is the
// difference between knowing it is up and knowing where to open it. Anything
// less certain than those two is left out rather than guessed at.

// portParam matches parameter names that hold a port: port, PORT, ports,
// http_port, web-port, webPort.
var portParam = regexp.MustCompile(`(?i:(?:^|[_.-])ports?$)|[a-z0-9]Ports?$`)

// maxPort is the highest number a TCP port can be. A parameter matching the
// name but not the range is some other kind of number.
const maxPort = 65535

// InstallationPorts returns the ports an installation answers on, lowest
// first, and nothing at all when neither the health check nor the parameters
// say. It is display information: no connection is made to find out.
func InstallationPorts(in *store.Installation) []int {
	if in == nil {
		return nil
	}

	var ports []int
	seen := map[int]bool{}
	add := func(p int) {
		if p <= 0 || p > maxPort || seen[p] {
			return
		}
		seen[p] = true
		ports = append(ports, p)
	}

	if in.HealthType == store.HealthHTTP {
		values := map[string]string{}
		for k, v := range in.Params {
			values[k] = v
		}
		values[VarHost] = in.HostAddress
		values[VarHostName] = in.HostName
		// A target that still has placeholders in it after this — {{user}},
		// say — renders to nothing usable, and no port is claimed for it.
		if target, err := Render(in.HealthTarget, values, false); err == nil {
			if p, ok := urlPort(target); ok {
				add(p)
			}
		}
	}

	for name, value := range in.Params {
		if !portParam.MatchString(name) {
			continue
		}
		if p, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			add(p)
		}
	}

	sort.Ints(ports)
	return ports
}

// urlPort is the port a health check URL dials: the one written into it, or
// the scheme's default when it is left out, because that is the port the app
// is answering on either way.
func urlPort(target string) (int, bool) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || u.Host == "" {
		return 0, false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return 80, true
	case "https":
		return 443, true
	}
	return 0, false
}
