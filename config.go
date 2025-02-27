// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of go-ra

package ra

import (
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"

	"github.com/creasty/defaults"
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// Config represents the configuration of the daemon
type Config struct {
	// Interface-specific configuration parameters. The Name field must be
	// unique within the slice. The slice itself and elements must not be
	// nil.
	Interfaces []*InterfaceConfig `yaml:"interfaces" json:"interfaces" validate:"required,non_nil_and_unique_name,dive,required"`
}

// InterfaceConfig represents the interface-specific configuration parameters
type InterfaceConfig struct {
	// Required: Network interface name. Must be unique within the configuration.
	Name string `yaml:"name" json:"name" validate:"required"`

	// Interval between sending unsolicited RA. Must be >= 70 and <= 1800000. Default is 600000.
	// The upper bound is chosen to be compliant with RFC4861. The lower bound is intentionally
	// chosen to be lower than RFC4861 for faster convergence. If you don't wish to overwhelm the
	// network, and wish to be compliant with RFC4861, set to higher than 3000 as RFC4861 suggests.
	RAIntervalMilliseconds int `yaml:"raIntervalMilliseconds" json:"raIntervalMilliseconds" validate:"required,gte=70,lte=1800000" default:"600000"`

	// RA header fields

	// The default value that should be placed in the Hop Count field of
	// the IP header for outgoing IP packets. Must be >= 0 and <= 255.
	// Default is 0. If set to zero, it means the reachable time is
	// unspecified by this router.
	CurrentHopLimit int `yaml:"currentHopLimit" json:"currentHopLimit" validate:"gte=0,lte=255" default:"0"`

	// Set M (Managed address configuration) flag. When set, it indicates
	// that addresses are available via DHCPv6. Default is false.
	Managed bool `yaml:"managed" json:"managed"`

	// Set O (Other configuration) flag. When set, it indicates that other
	// configuration information is available via DHCPv6. Default is false.
	Other bool `yaml:"other" json:"other"`

	// The lifetime associated with the default router in seconds. Must be
	// >= 0 and <= 65535. Default is 0. The upper bound is chosen to be
	// compliant to the RFC8319. If set to zero, the router is not
	// considered as a default router.
	RouterLifetimeSeconds int `yaml:"routerLifetimeSeconds" json:"routerLifetimeSeconds" validate:"gte=0,lte=65535"`

	// The time, in milliseconds, that a node assumes a neighbor is
	// reachable after having received a reachability confirmation. Must be
	// >= 0 and <= 4294967295. Default is 0. If set to zero, it means the
	// reachable time is unspecified by this router.
	ReachableTimeMilliseconds int `yaml:"reachableTimeMilliseconds" json:"reachableTimeMilliseconds" validate:"gte=0,lte=4294967295"`

	// The time, in milliseconds, between retransmitted Neighbor
	// Solicitation messages. Must be >= 0 and <= 4294967295. Default is 0.
	// If set to zero, it means the retransmission time is unspecified by
	// this router.
	RetransmitTimeMilliseconds int `yaml:"retransmitTimeMilliseconds" json:"retransmitTimeMilliseconds" validate:"gte=0,lte=4294967295"`

	// Prefix-specific configuration parameters. The prefix fields must be
	// non-overlapping with each other. The slice itself and elements must
	// not be nil.
	Prefixes []*PrefixConfig `yaml:"prefixes" json:"prefixes" validate:"non_nil_and_non_overlapping_prefix,dive,required"`
}

// PrefixConfig represents the prefix-specific configuration parameters
type PrefixConfig struct {
	// Required: Prefix. Must be a valid IPv6 prefix.
	Prefix string `yaml:"prefix" json:"prefix" validate:"required,cidrv6"`

	// Set L (On-Link) flag. When set, it indicates that this prefix can be
	// used for on-link determination. Default is false.
	OnLink bool `yaml:"onLink" json:"onLink"`

	// Set A (Autonomous address-configuration) flag. When set, it indicates
	// that this prefix can be used for stateless address autoconfiguration.
	// Default is false.
	Autonomous bool `yaml:"autonomous" json:"autonomous"`

	// The valid lifetime of the prefix in seconds. Must be >= 0 and <=
	// 4294967295 and must be >= PreferredLifetimeSeconds. Default is
	// 2592000 (30 days). If set to 4294967295, it indicates infinity.
	ValidLifetimeSeconds *int `yaml:"validLifetimeSeconds" json:"validLifetimeSeconds" validate:"required,gte=0,lte=4294967295" default:"2592000"`

	// The preferred lifetime of the prefix in seconds. Must be >= 0 and <=
	// 4294967295 and must be <= ValidLifetimeSeconds. Default is 604800 (7
	// days). If set to 4294967295, it indicates infinity.
	PreferredLifetimeSeconds *int `yaml:"preferredLifetimeSeconds" json:"preferredLifetimeSeconds" validate:"required,gte=0,ltefield=ValidLifetimeSeconds" default:"604800"`
}

// ValidationErrors is a type alias for the validator.ValidationErrors
type ValidationErrors = validator.ValidationErrors

func (c *Config) defaultAndValidate() error {
	if err := defaults.Set(c); err != nil {
		panic("BUG (Please report 🙏): Defaulting failed: " + err.Error())
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	// Adhoc custom validator which validates the slice elements are not
	// nil AND the Name field is unique. As far as I know, there is no way
	// to validate the uniqueness of struct fields in the nil-able slice of
	// struct pointerrs with validator's built-in constraints.
	validate.RegisterValidation("non_nil_and_unique_name", func(fl validator.FieldLevel) bool {
		names := make(map[string]struct{})

		ifaceSlice := fl.Field()
		for i := 0; i < fl.Field().Len(); i++ {
			ifacep := ifaceSlice.Index(i)
			if ifacep.IsNil() {
				return false
			}

			if ifacep.IsNil() {
				return false
			}

			iface := ifacep.Elem()

			name := iface.FieldByName("Name")
			if _, ok := names[name.String()]; ok {
				return false
			} else {
				names[name.String()] = struct{}{}
			}
		}

		return true
	})

	validate.RegisterValidation("non_nil_and_non_overlapping_prefix", func(fl validator.FieldLevel) bool {
		prefixes := []netip.Prefix{}

		prefixSlice := fl.Field()
		for i := 0; i < prefixSlice.Len(); i++ {
			prefixElemp := prefixSlice.Index(i)
			if prefixElemp.IsNil() {
				return false
			}

			prefixElem := prefixElemp.Elem()
			prefix := prefixElem.FieldByName("Prefix")

			p, err := netip.ParsePrefix(prefix.String())
			if err != nil {
				// Just ignore this error here. cidrv6 constraint will catch it later.
				continue
			}

			prefixes = append(prefixes, p)
		}

		// Check the prefix is not overlapping with each other
		for _, p0 := range prefixes {
			for _, p1 := range prefixes {
				if p0 != p1 && p0.Overlaps(p1) {
					return false
				}
			}
		}

		return true
	})

	if err := validate.Struct(c); err != nil {
		if _, ok := err.(*validator.InvalidValidationError); ok {
			panic("BUG (Please report 🙏): Invalid validation: " + err.Error())
		}

		var verrs ValidationErrors
		if errors.As(err, &verrs) {
			return verrs
		}

		// This is impossible, according to the validator's documentation
		// https://pkg.go.dev/github.com/go-playground/validator/v10#hdr-Validation_Functions_Return_Type_error
		return err
	}

	return nil
}

// ParseConfigJSON parses the JSON-encoded configuration from the reader. This
// function doesn't validate the configuration. The configuration is validated
// when you pass it to the Daemon.
func ParseConfigJSON(r io.Reader) (*Config, error) {
	var c Config

	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

// ParseConfigYAML parses the YAML-encoded configuration from the reader. This
// function doesn't validate the configuration. The configuration is validated
// when you pass it to the Daemon.
func ParseConfigYAML(r io.Reader) (*Config, error) {
	var c Config

	if err := yaml.NewDecoder(r).Decode(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

// ParseConfigYAMLFile parses the YAML-encoded configuration from the file at
// the given path. This function doesn't validate the configuration. The
// configuration is validated when you pass it to the Daemon.
func ParseConfigYAMLFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseConfigYAML(f)
}
