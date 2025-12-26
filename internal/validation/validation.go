package validation

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// hostnameRegex is a pre-compiled regex for RFC 1123 hostname validation
var hostnameRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// Error represents a validation error with an actionable remediation hint
type Error struct {
	Field       string
	Value       string
	Message     string
	Remediation string
}

func (e *Error) Error() string {
	if e.Remediation != "" {
		return fmt.Sprintf("%s: %s\nRemediation: %s", e.Field, e.Message, e.Remediation)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// CIDR validates a CIDR notation (e.g., "10.0.0.0/24")
func CIDR(field, value string) error {
	if value == "" {
		return nil // Empty values are handled by Required()
	}

	_, _, err := net.ParseCIDR(value)
	if err != nil {
		return &Error{
			Field:       field,
			Value:       value,
			Message:     fmt.Sprintf("invalid CIDR notation: %q", value),
			Remediation: "Provide a valid CIDR in the format: IP/prefix (e.g., 10.0.0.0/24, 192.168.1.0/24)",
		}
	}
	return nil
}

// Port validates a port number (1-65535)
func Port(field, value string) error {
	if value == "" {
		return nil // Empty values are handled by Required()
	}

	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return &Error{
			Field:       field,
			Value:       value,
			Message:     fmt.Sprintf("invalid port number: %q", value),
			Remediation: "Provide a valid port number between 1 and 65535",
		}
	}
	return nil
}

// IP validates an IP address (IPv4 or IPv6)
func IP(field, value string) error {
	if value == "" {
		return nil // Empty values are handled by Required()
	}

	if net.ParseIP(value) == nil {
		return &Error{
			Field:       field,
			Value:       value,
			Message:     fmt.Sprintf("invalid IP address: %q", value),
			Remediation: "Provide a valid IPv4 (e.g., 192.168.1.1) or IPv6 address",
		}
	}
	return nil
}

// Hostname validates a hostname or domain name
func Hostname(field, value string) error {
	if value == "" {
		return nil // Empty values are handled by Required()
	}

	// RFC 1123 hostname validation
	// - Max 253 characters total
	// - Each label max 63 characters
	// - Labels can contain alphanumeric and hyphens
	// - Labels cannot start or end with hyphen
	// - Labels must start with alphanumeric
	
	if len(value) > 253 || !hostnameRegex.MatchString(value) {
		return &Error{
			Field:       field,
			Value:       value,
			Message:     fmt.Sprintf("invalid hostname: %q", value),
			Remediation: "Provide a valid hostname/domain (e.g., example.com, kube.example.com)",
		}
	}
	return nil
}

// URL validates a URL
func URL(field, value string) error {
	if value == "" {
		return nil // Empty values are handled by Required()
	}

	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return &Error{
			Field:       field,
			Value:       value,
			Message:     fmt.Sprintf("invalid URL: %q", value),
			Remediation: "Provide a valid URL with scheme and host (e.g., https://192.168.1.1:6443)",
		}
	}
	return nil
}

// Required validates that a field is not empty
func Required(field, value string) error {
	if value == "" {
		return &Error{
			Field:       field,
			Value:       value,
			Message:     "field is required but not set",
			Remediation: fmt.Sprintf("Set %s via environment variable or command-line flag", field),
		}
	}
	return nil
}

// OneOf validates that a value is one of the allowed values
func OneOf(field, value string, allowed []string) error {
	if value == "" {
		return nil // Empty values are handled by Required()
	}

	for _, a := range allowed {
		if value == a {
			return nil
		}
	}

	return &Error{
		Field:       field,
		Value:       value,
		Message:     fmt.Sprintf("invalid value: %q", value),
		Remediation: fmt.Sprintf("Must be one of: %s", strings.Join(allowed, ", ")),
	}
}

// RequiredWith validates that if any of the fields are set, this field must also be set
func RequiredWith(field, value string, otherFields map[string]string) error {
	if value != "" {
		return nil // Field is set, validation passes
	}

	// Check if any of the other fields are set
	var setFields []string
	for k, v := range otherFields {
		if v != "" {
			setFields = append(setFields, k)
		}
	}

	if len(setFields) > 0 {
		return &Error{
			Field:       field,
			Value:       value,
			Message:     "field is required when other fields are set",
			Remediation: fmt.Sprintf("%s is required when %s is set", field, strings.Join(setFields, ", ")),
		}
	}

	return nil
}

// Errors collects multiple validation errors
type Errors []error

func (e Errors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var messages []string
	for _, err := range e {
		messages = append(messages, err.Error())
	}
	return fmt.Sprintf("validation failed:\n%s", strings.Join(messages, "\n"))
}

// HasErrors returns true if there are any errors
func (e Errors) HasErrors() bool {
	return len(e) > 0
}
