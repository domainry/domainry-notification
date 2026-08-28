package validation

import (
	"fmt"
	"strings"

	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
)

// Configuration contains immutable host-composed vocabulary needed to
// validate inbox contracts without hard-coding Plane product surfaces or a
// closed list of Connector channels.
type Configuration struct {
	surfaces         map[notification.Surface]bool
	externalChannels map[string]bool
}

func NewConfiguration(surfaces []notification.Surface, externalChannels []string) (*Configuration, error) {
	configuration := &Configuration{surfaces: map[notification.Surface]bool{}, externalChannels: map[string]bool{}}
	for _, surface := range surfaces {
		surface = notification.Surface(strings.TrimSpace(string(surface)))
		if surface == "" || configuration.surfaces[surface] {
			return nil, fmt.Errorf("notification inbox surface is empty or duplicated")
		}
		configuration.surfaces[surface] = true
	}
	if len(configuration.surfaces) == 0 {
		return nil, fmt.Errorf("notification inbox surfaces are required")
	}
	for _, channel := range externalChannels {
		channel = strings.TrimSpace(channel)
		if channel == "" || channel == "in_app" || configuration.externalChannels[channel] {
			return nil, fmt.Errorf("notification external channel is invalid or duplicated")
		}
		configuration.externalChannels[channel] = true
	}
	return configuration, nil
}

func (c *Configuration) SupportsSurface(surface notification.Surface) bool {
	return c != nil && c.surfaces[notification.Surface(strings.TrimSpace(string(surface)))]
}

func (c *Configuration) SupportsExternalChannel(channel string) bool {
	return c != nil && c.externalChannels[strings.TrimSpace(channel)]
}
