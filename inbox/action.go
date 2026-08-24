package inbox

// ActionRef is a semantic, server-authorized action. It is intentionally not
// an arbitrary URL or HTTP request.
type ActionRef struct {
	Key          string `json:"key"`
	Kind         string `json:"kind"`
	Label        string `json:"label"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Style        string `json:"style,omitempty"`
}

// ResolvedAction is safe navigation output for the current product surface.
// RouteKey is interpreted by the host router.
type ResolvedAction struct {
	Key            string            `json:"key"`
	Label          string            `json:"label"`
	Style          string            `json:"style,omitempty"`
	NavigationKind string            `json:"navigation_kind"`
	RouteKey       string            `json:"route_key"`
	RouteParams    map[string]string `json:"route_params"`
	Status         string            `json:"status"`
}

type ActionDescriptor struct {
	Key           string            `json:"key"`
	Kind          string            `json:"kind"`
	ResourceType  string            `json:"resource_type"`
	SurfaceRoutes map[string]string `json:"surface_routes"`
}
