package status

import "time"

// Indicator represents the severity level of a service or incident.
type Indicator string

const (
	IndicatorNone        Indicator = "none"        // All Systems Operational (Green)
	IndicatorMinor       Indicator = "minor"       // Minor Issue / Degraded (Amber)
	IndicatorMajor       Indicator = "major"       // Major Outage (Orange/Red)
	IndicatorCritical    Indicator = "critical"    // Critical Outage (Red)
	IndicatorMaintenance Indicator = "maintenance" // Scheduled Maintenance (Blue)
)

// Incident represents an individual outage, degradation, or maintenance event.
type Incident struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // investigating, identified, monitoring, resolved, completed, scheduled
	Impact    string    `json:"impact"` // none, minor, major, critical
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
	URL       string    `json:"shortlink"`
}

// Component represents a specific sub-feature or subsystem of a service.
type Component struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"` // operational, degraded_performance, partial_outage, major_outage
	Description string `json:"description,omitempty"`
}

// ServiceStatus represents the health and components of a specific Atlassian app.
type ServiceStatus struct {
	Name        string      `json:"name"`
	PageID      string      `json:"page_id"`
	Indicator   Indicator   `json:"indicator"`
	Description string      `json:"description"`
	Components  []Component `json:"components"`
}

// StatusReport provides an aggregated snapshot of all monitored services.
type StatusReport struct {
	OverallIndicator Indicator       `json:"overall_indicator"`
	OverallText      string          `json:"overall_text"`
	Services         []ServiceStatus `json:"services"`
	ActiveIncidents  []Incident      `json:"active_incidents"`
	LastChecked      time.Time       `json:"last_checked"`
	Error            string          `json:"error,omitempty"`
}
