package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// ResourceStatus holds detailed status for a specific resource
type ResourceStatus struct {
	Kind       string           `json:"kind"`
	Name       string           `json:"name"`
	Synced     string           `json:"synced"`
	Ready      string           `json:"ready"`
	Status     string           `json:"status"`
	Events     []string         `json:"events,omitempty"`
	Conditions []string         `json:"conditions,omitempty"`
	Children   []ResourceStatus `json:"children,omitempty"`
}

// CompositeData holds information about a single composite resource and its trace
type CompositeData struct {
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	TraceOutput string          `json:"trace_output,omitempty"` // Deprecated
	Error       string          `json:"error,omitempty"`
	Tree        *ResourceStatus `json:"tree,omitempty"`
}

// GenerateJSON writes the report in JSON format
func GenerateJSON(w io.Writer, data []CompositeData) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// GenerateCSV writes the report in CSV format
func GenerateCSV(w io.Writer, data []CompositeData) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header
	header := []string{
		"Root Name",
		"Parent Kind",
		"Parent Name",
		"Kind",
		"Name",
		"Status",
		"Synced",
		"Ready",
		"Details",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Rows
	for _, d := range data {
		if d.Tree != nil {
			// Traverse tree and write rows
			// Root has no parent
			if err := writeNodeRecursive(writer, d.Tree, d.Name, "", ""); err != nil {
				return err
			}
		} else {
			// Fallback for error cases or empty trees
			errRow := []string{d.Name, "", "", d.Kind, d.Name, "Error", "", "", d.Error}
			if err := writer.Write(errRow); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeNodeRecursive(writer *csv.Writer, node *ResourceStatus, rootName, parentKind, parentName string) error {
	// Format Details
	var details []string
	details = append(details, node.Conditions...)
	details = append(details, node.Events...)
	detailsStr := strings.Join(details, "; ")

	row := []string{
		rootName,
		parentKind,
		parentName,
		node.Kind,
		node.Name,
		node.Status,
		node.Synced,
		node.Ready,
		detailsStr,
	}

	if err := writer.Write(row); err != nil {
		return err
	}

	for _, child := range node.Children {
		if err := writeNodeRecursive(writer, &child, rootName, node.Kind, node.Name); err != nil {
			return err
		}
	}
	return nil
}

// GenerateTable writes the report in a pretty-printed table format
func GenerateTable(w io.Writer, data []CompositeData) error {
	// minwidth, tabwidth, padding, padchar, flags
	writer := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
	defer writer.Flush()

	// Header
	header := []string{
		"ROOT NAME",
		"PARENT KIND",
		"PARENT NAME",
		"KIND",
		"NAME",
		"STATUS",
		"SYNCED",
		"READY",
		"DETAILS",
	}
	// Join with tabs
	fmt.Fprintln(writer, strings.Join(header, "\t"))

	// Rows
	for _, d := range data {
		if d.Tree != nil {
			if err := writeNodeRecursiveTable(writer, d.Tree, d.Name, "", ""); err != nil {
				return err
			}
		} else {
			// Fallback
			row := []string{d.Name, "", "", d.Kind, d.Name, "Error", "", "", d.Error}
			fmt.Fprintln(writer, strings.Join(row, "\t"))
		}
	}
	return nil
}

func writeNodeRecursiveTable(writer *tabwriter.Writer, node *ResourceStatus, rootName, parentKind, parentName string) error {
	// Format Details
	var details []string
	details = append(details, node.Conditions...)
	details = append(details, node.Events...)
	detailsStr := strings.Join(details, "; ")

	row := []string{
		rootName,
		parentKind,
		parentName,
		node.Kind,
		node.Name,
		node.Status,
		node.Synced,
		node.Ready,
		detailsStr,
	}

	fmt.Fprintln(writer, strings.Join(row, "\t"))

	for _, child := range node.Children {
		if err := writeNodeRecursiveTable(writer, &child, rootName, node.Kind, node.Name); err != nil {
			return err
		}
	}
	return nil
}

// GetSummary returns a summary of the diagnosis and a boolean indicating if there are failures
func GetSummary(data []CompositeData) (string, bool) {
	var sb strings.Builder
	hasFailures := false

	fmt.Fprintln(&sb, "\n--- Summary ---")

	// Helper to collect unhealthy resources
	var collectUnhealthy func(*ResourceStatus) []ResourceStatus
	collectUnhealthy = func(node *ResourceStatus) []ResourceStatus {
		var unhealthy []ResourceStatus
		if node.Status != "Available" && node.Status != "Synced" {
			unhealthy = append(unhealthy, *node)
		}
		for _, child := range node.Children {
			unhealthy = append(unhealthy, collectUnhealthy(&child)...)
		}
		return unhealthy
	}

	failuresFound := false
	for _, d := range data {
		if d.Tree != nil {
			unhealthy := collectUnhealthy(d.Tree)
			if len(unhealthy) > 0 {
				failuresFound = true
				hasFailures = true
				fmt.Fprintf(&sb, "❌ Top Parent: %s/%s\n", d.Kind, d.Name)
				for _, res := range unhealthy {
					// Find the most relevant reason
					reason := "Unknown reason"
					if len(res.Conditions) > 0 {
						for _, cond := range res.Conditions {
							if strings.Contains(cond, "False") || strings.Contains(cond, "Unknown") {
								reason = cond
								break
							}
						}
						if reason == "Unknown reason" {
							reason = res.Conditions[0]
						}
					} else if len(res.Events) > 0 {
						reason = res.Events[0]
					}
					fmt.Fprintf(&sb, "  - Child %s/%s: %s\n    Reason: %s\n", res.Kind, res.Name, res.Status, reason)
				}
				fmt.Fprintln(&sb, "") // Empty line between parents
			}
		}
	}

	if !failuresFound {
		fmt.Fprintln(&sb, "✅ All resources are healthy!")
	}

	return sb.String(), hasFailures
}
