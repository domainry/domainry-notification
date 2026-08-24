package template

import (
	"context"
	"encoding/json"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/domainry/domainry-notification"
)

// Engine owns the in-memory published-template catalog and portable rendering.
// Provider payload compilation is deliberately outside this package.
type Engine struct {
	defaultLocale string
	validator     *Validator
	directory     RecipientDirectory
	templates     map[string]Template
	mu            sync.RWMutex
}

func NewEngine(defaultLocale string, templates []Template, validator *Validator, directory RecipientDirectory) (*Engine, error) {
	if validator == nil {
		return nil, notification.NewError(notification.ErrorUnavailable, "backend.notification.service_unavailable", nil, nil)
	}
	if err := validator.ValidateAll(templates); err != nil {
		return nil, err
	}
	engine := &Engine{defaultLocale: strings.TrimSpace(defaultLocale), validator: validator, directory: directory, templates: make(map[string]Template, len(templates))}
	for _, value := range templates {
		value = cloneTemplate(value)
		value.ContentHash = ContentHash(value)
		engine.templates[value.Key] = value
	}
	return engine, nil
}

func (e *Engine) Templates() []Template {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Template, 0, len(e.templates))
	for _, value := range e.templates {
		result = append(result, cloneTemplate(value))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func (e *Engine) Upsert(value Template) error {
	value.Status = "published"
	value = cloneTemplate(value)
	value.ContentHash = ContentHash(value)
	if err := e.validator.Validate(value); err != nil {
		return err
	}
	e.mu.Lock()
	e.templates[value.Key] = value
	e.mu.Unlock()
	return nil
}

func (e *Engine) Remove(key string) {
	e.mu.Lock()
	delete(e.templates, strings.TrimSpace(key))
	e.mu.Unlock()
}

func (e *Engine) Render(ctx context.Context, request RenderRequest) (Rendered, error) {
	e.mu.RLock()
	value, found := e.templates[strings.TrimSpace(request.TemplateKey)]
	e.mu.RUnlock()
	if !found {
		return Rendered{}, notification.NewError(notification.ErrorNotFound, "backend.notification.template_not_found", nil, map[string]any{"template_key": request.TemplateKey})
	}
	return e.render(ctx, value, request)
}

func (e *Engine) RenderTemplate(ctx context.Context, value Template, request RenderRequest) (Rendered, error) {
	value.Status = "published"
	value.ContentHash = ContentHash(value)
	if err := e.validator.Validate(value); err != nil {
		return Rendered{}, err
	}
	return e.render(ctx, value, request)
}

func (e *Engine) render(ctx context.Context, value Template, request RenderRequest) (Rendered, error) {
	locale, content := selectContent(value, request.Locale, e.defaultLocale)
	if locale == "" {
		return Rendered{}, invalid("backend.notification.template_locale_unavailable", "template_key", value.Key, "locale", request.Locale)
	}
	variables, err := ValidateRenderVariables(value, request.Variables)
	if err != nil {
		return Rendered{}, err
	}
	recipients, err := e.resolveRecipients(ctx, request.WorkspaceID, value.Channel, request.Recipients)
	if err != nil {
		return Rendered{}, err
	}
	subject, err := renderField(value.Key, "subject", content.Subject, variables, false)
	if err != nil {
		return Rendered{}, err
	}
	if strings.ContainsAny(subject, "\r\n") {
		return Rendered{}, invalid("backend.notification.subject_header_invalid", "template_key", value.Key)
	}
	text, err := renderField(value.Key, "text", content.Text, variables, false)
	if err != nil {
		return Rendered{}, err
	}
	htmlBody, err := renderField(value.Key, "html", content.HTML, variables, true)
	if err != nil {
		return Rendered{}, err
	}
	title, err := renderField(value.Key, "title", content.Title, variables, false)
	if err != nil {
		return Rendered{}, err
	}
	markdown, err := renderField(value.Key, "markdown", content.Markdown, variables, false)
	if err != nil {
		return Rendered{}, err
	}
	facts := make([]Fact, 0, len(content.Facts))
	for _, fact := range content.Facts {
		key, keyErr := RenderRestricted(fact.Key, variables, false)
		factValue, valueErr := RenderRestricted(fact.Value, variables, false)
		if keyErr != nil || valueErr != nil {
			return Rendered{}, invalid("backend.notification.template_render_failed", "template_key", value.Key, "field", "facts")
		}
		facts = append(facts, Fact{Key: key, Value: factValue})
	}
	actions := make([]Action, 0, len(content.Actions))
	for _, action := range content.Actions {
		label, labelErr := RenderRestricted(action.Label, variables, false)
		actionURL, urlErr := RenderRestricted(action.URL, variables, false)
		parsed, parseErr := url.Parse(actionURL)
		if labelErr != nil || urlErr != nil || parseErr != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return Rendered{}, invalid("backend.notification.template_action_invalid", "template_key", value.Key, "label", label)
		}
		actions = append(actions, Action{Label: label, URL: actionURL, Style: action.Style})
	}
	providerTemplate, err := RenderProviderTemplate(content.ProviderTemplate, variables)
	if err != nil {
		return Rendered{}, invalid("backend.notification.template_render_failed", "template_key", value.Key, "field", "provider_template")
	}
	message := text
	if strings.TrimSpace(markdown) != "" {
		message = markdown
	}
	if strings.TrimSpace(title) != "" {
		message = title + "\n\n" + message
	}
	if value.Channel == "email" {
		text, htmlBody = appendPortableEmail(text, htmlBody, facts, actions)
		message = text
	}
	return Rendered{
		Channel: value.Channel, Provider: value.Provider, Recipients: recipients,
		Subject: subject, Title: title, Text: text, HTML: htmlBody, Markdown: markdown, Message: message,
		Facts: facts, Actions: actions, ProviderTemplate: providerTemplate,
		TemplateKey: value.Key, TemplateVersion: value.Version, TemplateLocale: locale,
		TemplateContentHash: value.ContentHash, VariablesHash: ValueHash(variables), Metadata: request.Metadata,
		Fallbacks: append([]Fallback(nil), value.Fallbacks...),
	}, nil
}

func (e *Engine) resolveRecipients(ctx context.Context, workspaceID notification.WorkspaceID, channel string, values []notification.UserID) ([]string, error) {
	seen, result := map[string]bool{}, []string{}
	for _, userID := range values {
		if userID == "" {
			continue
		}
		address := userID.String()
		if channel == "email" {
			if e.directory == nil {
				return nil, invalid("backend.notification.recipient_directory_unavailable", "recipient", userID.String())
			}
			recipient, found, err := e.directory.FindRecipient(ctx, workspaceID, userID)
			if err != nil {
				return nil, err
			}
			if !found || strings.TrimSpace(recipient.Email) == "" {
				return nil, invalid("backend.notification.recipient_email_missing", "recipient", userID.String())
			}
			parsed, err := mail.ParseAddress(strings.TrimSpace(recipient.Email))
			if err != nil {
				return nil, invalid("backend.notification.recipient_email_invalid", "recipient", userID.String())
			}
			address = strings.ToLower(parsed.Address)
		}
		if !seen[address] {
			seen[address], result = true, append(result, address)
		}
	}
	if len(result) == 0 {
		return nil, invalid("backend.notification.recipients_required")
	}
	return result, nil
}

func selectContent(value Template, requestedLocale, runtimeLocale string) (string, Content) {
	for _, locale := range []string{strings.TrimSpace(requestedLocale), strings.TrimSpace(runtimeLocale), strings.TrimSpace(value.DefaultLocale)} {
		if content, found := value.Locales[locale]; found {
			return locale, content
		}
	}
	return "", Content{}
}

func renderField(templateKey, field, source string, variables map[string]any, escapeHTML bool) (string, error) {
	value, err := RenderRestricted(source, variables, escapeHTML)
	if err != nil {
		return "", invalid("backend.notification.template_render_failed", "template_key", templateKey, "field", field)
	}
	return value, nil
}

func cloneTemplate(value Template) Template {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var result Template
	if json.Unmarshal(raw, &result) != nil {
		return value
	}
	return result
}
