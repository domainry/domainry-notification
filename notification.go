// Package notification contains the stable domain language shared by the
// notification capabilities in this module.
//
// Behavior is grouped by capability in the template, inbox, and delivery
// packages. Host dependencies are declared by the capability that consumes
// them, and SQL-backed persistence is implemented by package sqlstore.
package notification
