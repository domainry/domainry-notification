package model

// Recipient is the minimum identity projection needed for rendering and
// delivery policy. Hosts must not expose their complete user aggregate here.
type Recipient struct {
	ID       UserID
	Email    string
	Locale   string
	Timezone string
}
