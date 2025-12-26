package validation

import (
	"strings"
	"testing"
)

func TestCIDR(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{
			name:    "valid IPv4 CIDR",
			field:   "SERVICE_CIDR",
			value:   "10.43.0.0/16",
			wantErr: false,
		},
		{
			name:    "valid IPv4 CIDR /24",
			field:   "PRIVATE_CIDR",
			value:   "192.168.1.0/24",
			wantErr: false,
		},
		{
			name:    "valid IPv4 CIDR /32",
			field:   "ADMIN_SRC_CIDR",
			value:   "203.0.113.1/32",
			wantErr: false,
		},
		{
			name:    "valid IPv6 CIDR",
			field:   "CLUSTER_CIDR",
			value:   "fd00::/64",
			wantErr: false,
		},
		{
			name:    "empty value",
			field:   "PRIVATE_CIDR",
			value:   "",
			wantErr: false, // Empty is allowed, Required() handles this
		},
		{
			name:    "invalid CIDR - no prefix",
			field:   "PRIVATE_CIDR",
			value:   "192.168.1.0",
			wantErr: true,
		},
		{
			name:    "invalid CIDR - bad prefix",
			field:   "PRIVATE_CIDR",
			value:   "192.168.1.0/33",
			wantErr: true,
		},
		{
			name:    "invalid CIDR - bad IP",
			field:   "PRIVATE_CIDR",
			value:   "999.999.999.999/24",
			wantErr: true,
		},
		{
			name:    "invalid CIDR - random text",
			field:   "PRIVATE_CIDR",
			value:   "not-a-cidr",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CIDR(tt.field, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("CIDR() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("CIDR() error is not *Error type")
				}
				if valErr.Field != tt.field {
					t.Errorf("CIDR() error field = %v, want %v", valErr.Field, tt.field)
				}
				if valErr.Remediation == "" {
					t.Errorf("CIDR() error missing remediation")
				}
			}
		})
	}
}

func TestPort(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{
			name:    "valid port 80",
			field:   "TRAEFIK_NODEPORT_HTTP",
			value:   "80",
			wantErr: false,
		},
		{
			name:    "valid port 443",
			field:   "TRAEFIK_NODEPORT_HTTPS",
			value:   "443",
			wantErr: false,
		},
		{
			name:    "valid port 30080",
			field:   "TRAEFIK_NODEPORT_HTTP",
			value:   "30080",
			wantErr: false,
		},
		{
			name:    "valid port 65535",
			field:   "PORT",
			value:   "65535",
			wantErr: false,
		},
		{
			name:    "empty value",
			field:   "PORT",
			value:   "",
			wantErr: false, // Empty is allowed
		},
		{
			name:    "invalid port 0",
			field:   "PORT",
			value:   "0",
			wantErr: true,
		},
		{
			name:    "invalid port 65536",
			field:   "PORT",
			value:   "65536",
			wantErr: true,
		},
		{
			name:    "invalid port negative",
			field:   "PORT",
			value:   "-1",
			wantErr: true,
		},
		{
			name:    "invalid port text",
			field:   "PORT",
			value:   "not-a-port",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Port(tt.field, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Port() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("Port() error is not *Error type")
				}
				if valErr.Remediation == "" {
					t.Errorf("Port() error missing remediation")
				}
			}
		})
	}
}

func TestIP(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{
			name:    "valid IPv4",
			field:   "NODE_IP",
			value:   "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "valid IPv4 127.0.0.1",
			field:   "NODE_IP",
			value:   "127.0.0.1",
			wantErr: false,
		},
		{
			name:    "valid IPv6",
			field:   "NODE_IP",
			value:   "2001:db8::1",
			wantErr: false,
		},
		{
			name:    "valid IPv6 loopback",
			field:   "NODE_IP",
			value:   "::1",
			wantErr: false,
		},
		{
			name:    "empty value",
			field:   "NODE_IP",
			value:   "",
			wantErr: false,
		},
		{
			name:    "invalid IP - with CIDR",
			field:   "NODE_IP",
			value:   "192.168.1.1/24",
			wantErr: true,
		},
		{
			name:    "invalid IP - bad octets",
			field:   "NODE_IP",
			value:   "999.999.999.999",
			wantErr: true,
		},
		{
			name:    "invalid IP - text",
			field:   "NODE_IP",
			value:   "not-an-ip",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IP(tt.field, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("IP() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("IP() error is not *Error type")
				}
				if valErr.Remediation == "" {
					t.Errorf("IP() error missing remediation")
				}
			}
		})
	}
}

func TestHostname(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{
			name:    "valid hostname",
			field:   "BASE_DOMAIN",
			value:   "example.com",
			wantErr: false,
		},
		{
			name:    "valid subdomain",
			field:   "DASH_HOST",
			value:   "kube.example.com",
			wantErr: false,
		},
		{
			name:    "valid multi-level subdomain",
			field:   "DASH_HOST",
			value:   "dashboard.kube.example.com",
			wantErr: false,
		},
		{
			name:    "valid with hyphen",
			field:   "BASE_DOMAIN",
			value:   "my-domain.com",
			wantErr: false,
		},
		{
			name:    "valid single label",
			field:   "HOSTNAME",
			value:   "localhost",
			wantErr: false,
		},
		{
			name:    "empty value",
			field:   "BASE_DOMAIN",
			value:   "",
			wantErr: false,
		},
		{
			name:    "invalid - starts with hyphen",
			field:   "BASE_DOMAIN",
			value:   "-example.com",
			wantErr: true,
		},
		{
			name:    "invalid - ends with hyphen",
			field:   "BASE_DOMAIN",
			value:   "example-.com",
			wantErr: true,
		},
		{
			name:    "invalid - contains underscore",
			field:   "BASE_DOMAIN",
			value:   "example_domain.com",
			wantErr: true,
		},
		{
			name:    "invalid - too long",
			field:   "BASE_DOMAIN",
			value:   strings.Repeat("a", 254) + ".com",
			wantErr: true,
		},
		{
			name:    "invalid - IP address",
			field:   "BASE_DOMAIN",
			value:   "192.168.1.1",
			wantErr: false, // IP addresses can be valid hostnames per RFC 1123
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Hostname(tt.field, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Hostname() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("Hostname() error is not *Error type")
				}
				if valErr.Remediation == "" {
					t.Errorf("Hostname() error missing remediation")
				}
			}
		})
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{
			name:    "valid HTTPS URL",
			field:   "SERVER_URL",
			value:   "https://192.168.1.1:6443",
			wantErr: false,
		},
		{
			name:    "valid HTTP URL",
			field:   "EDGE_UPSTREAM",
			value:   "http://127.0.0.1:30080",
			wantErr: false,
		},
		{
			name:    "valid URL with hostname",
			field:   "SERVER_URL",
			value:   "https://kube.example.com:6443",
			wantErr: false,
		},
		{
			name:    "empty value",
			field:   "SERVER_URL",
			value:   "",
			wantErr: false,
		},
		{
			name:    "invalid - no scheme",
			field:   "SERVER_URL",
			value:   "192.168.1.1:6443",
			wantErr: true,
		},
		{
			name:    "invalid - no host",
			field:   "SERVER_URL",
			value:   "https://",
			wantErr: true,
		},
		{
			name:    "invalid - malformed",
			field:   "SERVER_URL",
			value:   "not a url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := URL(tt.field, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("URL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("URL() error is not *Error type")
				}
				if valErr.Remediation == "" {
					t.Errorf("URL() error missing remediation")
				}
			}
		})
	}
}

func TestRequired(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		{
			name:    "value is set",
			field:   "BASE_DOMAIN",
			value:   "example.com",
			wantErr: false,
		},
		{
			name:    "value is empty",
			field:   "BASE_DOMAIN",
			value:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Required(tt.field, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Required() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("Required() error is not *Error type")
				}
				if valErr.Remediation == "" {
					t.Errorf("Required() error missing remediation")
				}
			}
		})
	}
}

func TestOneOf(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		allowed []string
		wantErr bool
	}{
		{
			name:    "value is in allowed list",
			field:   "MODE",
			value:   "bootstrap",
			allowed: []string{"bootstrap", "join"},
			wantErr: false,
		},
		{
			name:    "value is not in allowed list",
			field:   "MODE",
			value:   "invalid",
			allowed: []string{"bootstrap", "join"},
			wantErr: true,
		},
		{
			name:    "empty value",
			field:   "MODE",
			value:   "",
			allowed: []string{"bootstrap", "join"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := OneOf(tt.field, tt.value, tt.allowed)
			if (err != nil) != tt.wantErr {
				t.Errorf("OneOf() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("OneOf() error is not *Error type")
				}
				if valErr.Remediation == "" {
					t.Errorf("OneOf() error missing remediation")
				}
			}
		})
	}
}

func TestRequiredWith(t *testing.T) {
	tests := []struct {
		name        string
		field       string
		value       string
		otherFields map[string]string
		wantErr     bool
	}{
		{
			name:  "field is set",
			field: "PRIVATE_CIDR",
			value: "10.10.0.0/24",
			otherFields: map[string]string{
				"ENABLE_VLAN_NAT": "true",
			},
			wantErr: false,
		},
		{
			name:  "field is not set but required",
			field: "PRIVATE_CIDR",
			value: "",
			otherFields: map[string]string{
				"ENABLE_VLAN_NAT": "true",
			},
			wantErr: true,
		},
		{
			name:  "field is not set and not required",
			field: "PRIVATE_CIDR",
			value: "",
			otherFields: map[string]string{
				"ENABLE_VLAN_NAT": "",
			},
			wantErr: false,
		},
		{
			name:        "no other fields",
			field:       "PRIVATE_CIDR",
			value:       "",
			otherFields: map[string]string{},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequiredWith(tt.field, tt.value, tt.otherFields)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequiredWith() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				valErr, ok := err.(*Error)
				if !ok {
					t.Errorf("RequiredWith() error is not *Error type")
				}
				if valErr.Remediation == "" {
					t.Errorf("RequiredWith() error missing remediation")
				}
			}
		})
	}
}

func TestErrors(t *testing.T) {
	t.Run("no errors", func(t *testing.T) {
		errs := Errors{}
		if errs.HasErrors() {
			t.Errorf("HasErrors() = true, want false")
		}
		if errs.Error() != "" {
			t.Errorf("Error() = %q, want empty string", errs.Error())
		}
	})

	t.Run("with errors", func(t *testing.T) {
		errs := Errors{
			&Error{Field: "FIELD1", Message: "error 1"},
			&Error{Field: "FIELD2", Message: "error 2"},
		}
		if !errs.HasErrors() {
			t.Errorf("HasErrors() = false, want true")
		}
		errStr := errs.Error()
		if errStr == "" {
			t.Errorf("Error() returned empty string")
		}
		if !strings.Contains(errStr, "FIELD1") || !strings.Contains(errStr, "FIELD2") {
			t.Errorf("Error() = %q, should contain both field names", errStr)
		}
	})
}

func TestError_Error(t *testing.T) {
	t.Run("with remediation", func(t *testing.T) {
		err := &Error{
			Field:       "BASE_DOMAIN",
			Value:       "invalid",
			Message:     "invalid domain",
			Remediation: "Provide a valid domain",
		}
		errStr := err.Error()
		if !strings.Contains(errStr, "BASE_DOMAIN") {
			t.Errorf("Error() should contain field name")
		}
		if !strings.Contains(errStr, "Remediation") {
			t.Errorf("Error() should contain remediation")
		}
	})

	t.Run("without remediation", func(t *testing.T) {
		err := &Error{
			Field:   "BASE_DOMAIN",
			Value:   "invalid",
			Message: "invalid domain",
		}
		errStr := err.Error()
		if !strings.Contains(errStr, "BASE_DOMAIN") {
			t.Errorf("Error() should contain field name")
		}
		if strings.Contains(errStr, "Remediation") {
			t.Errorf("Error() should not contain remediation when not set")
		}
	})
}
