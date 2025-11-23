# Crossplane Diagnose CLI

**crossplane-diagnose** is a powerful, standalone CLI tool designed to help SREs and Platform Engineers instantly identify and resolve issues with Crossplane resources.

It goes beyond standard `kubectl` output by building a full dependency tree of your Composite Resources (XRs), analyzing their health, and providing actionable AI-driven debugging steps.

## 🚀 Features

- **🌲 Native Resource Tree**: Automatically builds a hierarchical tree of your Composite Resources and their dependencies (Claims -> XR -> Composition -> Managed Resources) using native Kubernetes discovery.
- **🤖 AI-Powered Analysis**: Integrates with **Claude** (via `claude` CLI) to analyze failure patterns and suggest specific fixes, `kubectl` commands, and root causes.
- **🔍 Deep Diagnostics**: Fetches Kubernetes **Events** and **Status Conditions** for every unhealthy resource to pinpoint exactly *why* something failed.
- **📊 Rich Reporting**:
  - **Table**: Pretty-printed terminal output for quick scanning.
  - **JSON**: Full structured data for automated processing.
  - **CSV**: Flattened export for spreadsheet analysis.
- **🎯 Smart Filtering**:
  - Filter by **Resource Name** (`--resource`).
  - Filter by **Kind** (`--kind`).
  - Automatically hides redundant child resources from the top-level view.
- **⚡ Standalone**: Written in Go, it runs without needing `kubectl` or `crossplane` CLI installed (uses your kubeconfig directly).

## 📦 Installation

### Build from Source
```bash
git clone https://github.com/vinish86/crossplane-diagnose.git
cd crossplane-diagnose
go build -o crossplane-diagnose .
```

## 🛠 Usage

Ensure your `KUBECONFIG` is set or you have a valid `~/.kube/config`.

### Basic Diagnosis (Table Output)
```bash
./crossplane-diagnose --output table
```

### AI-Powered Analysis 🤖
Pipe the failure summary to Claude for expert debugging advice:
```bash
./crossplane-diagnose --ai-analysis --ai-provider claude
```
*(Requires `claude` CLI to be installed and authenticated)*

### Custom AI Prompt
You can provide your own system prompt to tailor the AI's analysis.
See `examples/prompt.txt` for a template.

```bash
./crossplane-diagnose --ai-analysis --ai-prompt-file examples/prompt.txt
```

### Filter by Resource Kind
Diagnose all `XPostgreSQLInstance` resources:
```bash
./crossplane-diagnose --kind XPostgreSQLInstance --output table
```

### Ignore Specific Kinds
Some resources (like `Usage` or `EnvironmentConfig`) might not report status correctly or are irrelevant for health checks.
By default, the tool treats `Usage` and `EnvironmentConfig` as "Available".
You can override this list:
```bash
./crossplane-diagnose --ignore-kinds Usage,EnvironmentConfig,MyCustomKind
```

### Filter by Resource Name
Diagnose a specific resource tree:
```bash
./crossplane-diagnose --resource my-db-instance --output json
```

## 🧠 How It Works

1. **Discovery**: The tool uses the Kubernetes Discovery API to find all resources marked with the `composite` category.
2. **Tree Building**: It recursively traverses `spec.resourceRefs` to build a complete dependency graph for each Composite Resource.
3. **Health Check**: It evaluates the `Ready` and `Synced` conditions of every resource in the tree.
4. **Deep Analysis**: For any unhealthy resource, it fetches relevant Kubernetes Events and detailed Status Conditions.
5. **Reporting**: It aggregates this data into a structured report and, optionally, sends a summary to an AI provider for interpretation.

## 💡 Benefits

| Feature | Standard `kubectl` | `crossplane-diagnose` |
| :--- | :--- | :--- |
| **Visibility** | Flat list of resources | Full hierarchical tree |
| **Debugging** | Manual `describe` on each resource | Auto-fetches Events & Conditions for failures |
| **Context** | Isolated errors | Errors grouped by Parent Composite |
| **Analysis** | You figure it out | AI suggests fixes and commands |
| **Speed** | Slow manual correlation | Instant snapshot of cluster health |

## 🤝 Contributing

Pull requests are welcome! Feel free to open an issue if you find a bug or have a feature request.
