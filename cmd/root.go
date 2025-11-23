package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vinishsoman/crossplane-diagnose/pkg/ai"
	"github.com/vinishsoman/crossplane-diagnose/pkg/report"
	"github.com/vinishsoman/crossplane-diagnose/pkg/tree"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var (
	outputFormat string
	aiAnalysis   bool
	resourceName string
	resourceKind string
	aiProvider   string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "crossplane-diagnose",
	Short: "A CLI tool to diagnose Crossplane issues",
	Long: `crossplane-diagnose is a CLI tool designed to help you identify and resolve 
issues with your Crossplane installation and resources. It builds a resource tree 
for each Composite Resource and generates a detailed report.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(os.Stderr, "Starting Crossplane diagnosis...\n")

		// 1. Initialize Dynamic Client
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			if home := homedir.HomeDir(); home != "" {
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		}

		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error building kubeconfig: %v\n", err)
			return
		}

		dynClient, err := dynamic.NewForConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating dynamic client: %v\n", err)
			return
		}

		treeBuilder := tree.NewBuilder(dynClient)

		// 2. Discover and List all composites
		fmt.Fprintf(os.Stderr, "Discovering composite resources...\n")

		discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating discovery client: %v\n", err)
			return
		}

		// Find all GVRs with category "composite"
		var compositeGVRs []schema.GroupVersionResource
		groups, err := discoveryClient.ServerGroups()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching server groups: %v\n", err)
			return
		}

		for _, group := range groups.Groups {
			for _, version := range group.Versions {
				gv := schema.GroupVersion{Group: group.Name, Version: version.Version}
				resources, err := discoveryClient.ServerResourcesForGroupVersion(gv.String())
				if err != nil {
					// Ignore errors for specific versions (e.g. if CRD is broken)
					continue
				}

				for _, r := range resources.APIResources {
					for _, category := range r.Categories {
						if category == "composite" {
							compositeGVRs = append(compositeGVRs, gv.WithResource(r.Name))
							break
						}
					}
				}
			}
		}

		fmt.Fprintf(os.Stderr, "Found %d composite types. Listing resources...\n", len(compositeGVRs))

		type CompositeItem struct {
			APIVersion string
			Kind       string
			Name       string
		}
		var allItems []CompositeItem

		for _, gvr := range compositeGVRs {
			list, err := dynClient.Resource(gvr).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listing %s: %v\n", gvr.String(), err)
				continue
			}

			for _, item := range list.Items {
				allItems = append(allItems, CompositeItem{
					APIVersion: item.GetAPIVersion(),
					Kind:       item.GetKind(),
					Name:       item.GetName(),
				})
			}
		}

		// Filter list if resourceName or resourceKind is provided
		if resourceName != "" || resourceKind != "" {
			var filteredItems []CompositeItem
			found := false
			for _, item := range allItems {
				match := true
				if resourceName != "" && item.Name != resourceName {
					match = false
				}
				if resourceKind != "" && !strings.EqualFold(item.Kind, resourceKind) {
					match = false
				}

				if match {
					filteredItems = append(filteredItems, item)
					found = true
				}
			}

			if !found {
				msg := "No resources found matching"
				if resourceName != "" {
					msg += fmt.Sprintf(" name='%s'", resourceName)
				}
				if resourceKind != "" {
					msg += fmt.Sprintf(" kind='%s'", resourceKind)
				}
				fmt.Fprintf(os.Stderr, "Warning: %s.\n", msg)
				allItems = []CompositeItem{}
			} else {
				allItems = filteredItems
			}
		}

		fmt.Fprintf(os.Stderr, "Found %d composites. Building trees...\n", len(allItems))

		var results []report.CompositeData

		// 3. Build tree for each composite
		for _, item := range allItems {
			fmt.Fprintf(os.Stderr, "Analyzing %s/%s...\n", item.Kind, item.Name)

			gv, err := schema.ParseGroupVersion(item.APIVersion)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Error parsing APIVersion %s: %v\n", item.APIVersion, err)
				continue
			}

			// We need the plural resource.
			// Since we listed "composite", these are XRs.
			// We can guess plural or just use the Kind if we had a mapper.
			// For the initial fetch of the XR, we can use the GVR if we knew the resource name.
			// But wait, we already have the item from kubectl.
			// We can just pass the GVR to BuildTree.
			// To get the GVR resource name (plural), we can try lowercase + s.
			resource := strings.ToLower(item.Kind) + "s"

			gvr := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: resource,
			}

			root, err := treeBuilder.BuildTree(context.Background(), gvr, item.Name)
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}

			results = append(results, report.CompositeData{
				Name:  item.Name,
				Kind:  item.Kind,
				Tree:  root,
				Error: errStr,
			})
		}

		// 4. Filter Redundant Resources
		// Identify all resources that appear as children in any tree
		childResources := make(map[string]bool)
		for _, res := range results {
			if res.Tree != nil {
				collectChildren(res.Tree, childResources)
			}
		}

		// Filter out top-level items that are children
		var filteredResults []report.CompositeData
		for _, res := range results {
			key := fmt.Sprintf("%s/%s", res.Kind, res.Name)
			if !childResources[key] {
				filteredResults = append(filteredResults, res)
			} else {
				// Optional: Log that we are hiding a resource?
				// fmt.Fprintf(os.Stderr, "Hiding child resource from top-level: %s\n", key)
			}
		}

		// 5. Generate Report
		var genErr error
		switch strings.ToLower(outputFormat) {
		case "json":
			genErr = report.GenerateJSON(os.Stdout, filteredResults)
		case "csv":
			genErr = report.GenerateCSV(os.Stdout, filteredResults)
		case "table":
			genErr = report.GenerateTable(os.Stdout, filteredResults)
		default:
			fmt.Fprintf(os.Stderr, "Unknown output format '%s', defaulting to JSON\n", outputFormat)
			genErr = report.GenerateJSON(os.Stdout, filteredResults)
		}

		if genErr != nil {
			fmt.Fprintf(os.Stderr, "Error generating report: %v\n", genErr)
		}

		// 6. Print Summary and AI Analysis
		summary, hasFailures := report.GetSummary(filteredResults)
		fmt.Fprint(os.Stderr, summary)

		if hasFailures && aiAnalysis {
			fmt.Fprintf(os.Stderr, "\n🤖 Sending failure summary to %s for analysis...\n", aiProvider)

			var cmdAI *exec.Cmd
			prompt := ai.ConstructPrompt(summary)

			switch strings.ToLower(aiProvider) {
			case "claude":
				// Use -p flag for non-interactive mode
				cmdAI = exec.Command("claude", "-p", prompt)
			default:
				fmt.Fprintf(os.Stderr, "Error: Unknown AI provider '%s'. Supported providers: claude\n", aiProvider)
				return
			}

			// We want to stream output to stdout/stderr
			cmdAI.Stdout = os.Stdout
			cmdAI.Stderr = os.Stderr

			if err := cmdAI.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running AI analysis: %v\n", err)
			}
		}
	},
}

func collectChildren(node *report.ResourceStatus, children map[string]bool) {
	for _, child := range node.Children {
		key := fmt.Sprintf("%s/%s", child.Kind, child.Name)
		children[key] = true
		collectChildren(&child, children)
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&outputFormat, "output", "o", "json", "Output format (json, csv, table)")
	rootCmd.Flags().BoolVar(&aiAnalysis, "ai-analysis", false, "Send failure summary to AI provider for analysis")
	rootCmd.Flags().StringVar(&aiProvider, "ai-provider", "claude", "AI provider to use for analysis (claude)")
	rootCmd.Flags().StringVarP(&resourceName, "resource", "r", "", "Name of the specific composite resource to diagnose")
	rootCmd.Flags().StringVarP(&resourceKind, "kind", "k", "", "Kind of the composite resources to diagnose (case-insensitive)")
}
