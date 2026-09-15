package security

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// ParseOrigin validates without DNS resolution or reflecting supplied values.
// Transport and authentication policies are enforced by its callers.
func ParseOrigin(value string) (*url.URL, error) {
	invalid := errors.New("server must be an HTTP(S) origin without credentials, paths, queries, fragments, or IPv6 zones")
	origin, err := url.Parse(value)
	if err != nil || origin.Opaque != "" || (origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.User != nil || (origin.Path != "" && origin.Path != "/") || origin.RawPath != "" ||
		origin.RawQuery != "" || origin.ForceQuery || strings.Contains(value, "#") {
		return nil, invalid
	}
	host := strings.ToLower(origin.Hostname())
	if ip, err := netip.ParseAddr(host); err == nil {
		if strings.HasPrefix(origin.Host, "[") && !ip.Is6() {
			return nil, invalid
		}
		if ip.Zone() != "" || ip.Unmap().IsUnspecified() || ip.Unmap().IsMulticast() {
			return nil, invalid
		}
	} else {
		if strings.HasPrefix(origin.Host, "[") || len(host) == 0 || len(host) > 253 {
			return nil, invalid
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return nil, invalid
			}
			for _, char := range label {
				if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
					return nil, invalid
				}
			}
		}
	}
	if port := origin.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, invalid
		}
	} else if strings.HasSuffix(origin.Host, ":") {
		return nil, invalid
	}
	origin.Path = ""
	return origin, nil
}

func IsLoopback(origin *url.URL) bool {
	if strings.EqualFold(origin.Hostname(), "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(origin.Hostname())
	return err == nil && ip.Unmap().IsLoopback()
}
