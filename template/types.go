package template

// Template is an immutable published message contract. Version and ContentHash
// pin the exact content used to produce retry-stable delivery snapshots.
type Template struct {
	Key           string             `json:"key"`
	Name          string             `json:"name"`
	Channel       string             `json:"channel"`
	Provider      string             `json:"provider,omitempty"`
	Status        string             `json:"status"`
	Version       int                `json:"version"`
	DefaultLocale string             `json:"default_locale"`
	Variables     []Variable         `json:"variables,omitempty"`
	Fallbacks     []Fallback         `json:"fallbacks,omitempty"`
	Locales       map[string]Content `json:"locales"`
	ContentHash   string             `json:"content_hash,omitempty"`
}

type Fallback struct {
	TemplateKey   string `json:"template_key"`
	ConnectorKey  string `json:"connector_key"`
	ConnectionKey string `json:"connection_key"`
	Operation     string `json:"operation"`
}

type Variable struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

type Content struct {
	Subject          string            `json:"subject"`
	Title            string            `json:"title,omitempty"`
	Text             string            `json:"text,omitempty"`
	HTML             string            `json:"html,omitempty"`
	Markdown         string            `json:"markdown,omitempty"`
	Facts            []Fact            `json:"facts,omitempty"`
	Actions          []Action          `json:"actions,omitempty"`
	ProviderTemplate *ProviderTemplate `json:"provider_template,omitempty"`
}

type ProviderTemplate struct {
	Name       string                      `json:"name"`
	Language   string                      `json:"language"`
	Components []ProviderTemplateComponent `json:"components,omitempty"`
}

type ProviderTemplateComponent struct {
	Type       string   `json:"type"`
	SubType    string   `json:"sub_type,omitempty"`
	Index      string   `json:"index,omitempty"`
	Parameters []string `json:"parameters,omitempty"`
}

type Fact struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Action is portable rendered content. Inbox actions are separate semantic,
// host-authorized actions and never persist arbitrary HTTP requests.
type Action struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Style string `json:"style,omitempty"`
}

type Rendered struct {
	Channel             string            `json:"channel"`
	Provider            string            `json:"provider,omitempty"`
	Recipients          []string          `json:"recipients"`
	Subject             string            `json:"subject"`
	Title               string            `json:"title,omitempty"`
	Text                string            `json:"text,omitempty"`
	HTML                string            `json:"html,omitempty"`
	Markdown            string            `json:"markdown,omitempty"`
	Message             string            `json:"message,omitempty"`
	Facts               []Fact            `json:"facts,omitempty"`
	Actions             []Action          `json:"actions,omitempty"`
	ProviderTemplate    *ProviderTemplate `json:"provider_template,omitempty"`
	TemplateKey         string            `json:"template_key"`
	TemplateVersion     int               `json:"template_version"`
	TemplateLocale      string            `json:"template_locale"`
	TemplateContentHash string            `json:"template_content_hash"`
	VariablesHash       string            `json:"variables_hash"`
	Metadata            map[string]any    `json:"metadata,omitempty"`
	Fallbacks           []Fallback        `json:"fallbacks,omitempty"`
}
