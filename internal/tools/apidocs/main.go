package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type openAPIDocument struct {
	Tags []struct {
		Name string `yaml:"name"`
	} `yaml:"tags"`
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

type operation struct {
	Tags        []string `yaml:"tags"`
	OperationID string   `yaml:"operationId"`
	Summary     string   `yaml:"summary"`
}

type endpoint struct {
	method      string
	path        string
	operationID string
	summary     string
}

func main() {
	input := flag.String("input", "api/openapi.yaml", "OpenAPI input file")
	output := flag.String("output", "docs/api.md", "Markdown output file")
	flag.Parse()

	data, err := os.ReadFile(*input)
	if err != nil {
		fatal(err)
	}
	rendered, err := generate(data)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "apidocs:", err)
	os.Exit(1)
}

func generate(data []byte) ([]byte, error) {
	var document openAPIDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse OpenAPI document: %w", err)
	}
	if len(document.Paths) == 0 {
		return nil, errors.New("OpenAPI document has no paths")
	}

	endpointsByTag := make(map[string][]endpoint)
	for path, pathItem := range document.Paths {
		for method, node := range pathItem {
			if method == "parameters" {
				continue
			}
			var op operation
			if err := node.Decode(&op); err != nil {
				return nil, fmt.Errorf("decode %s %s: %w", strings.ToUpper(method), path, err)
			}
			if op.OperationID == "" {
				return nil, fmt.Errorf("%s %s has no operationId", strings.ToUpper(method), path)
			}
			if len(op.Tags) == 0 {
				return nil, fmt.Errorf("%s %s has no tags", op.OperationID, path)
			}
			for _, tag := range op.Tags {
				endpointsByTag[tag] = append(endpointsByTag[tag], endpoint{
					method:      strings.ToUpper(method),
					path:        path,
					operationID: op.OperationID,
					summary:     cleanSummary(op.Summary),
				})
			}
		}
	}

	tagOrder := make([]string, 0, len(document.Tags)+len(endpointsByTag))
	seenTags := make(map[string]struct{})
	for _, tag := range document.Tags {
		if _, seen := seenTags[tag.Name]; seen {
			continue
		}
		seenTags[tag.Name] = struct{}{}
		tagOrder = append(tagOrder, tag.Name)
	}
	var unknownTags []string
	for tag := range endpointsByTag {
		if _, known := seenTags[tag]; !known {
			unknownTags = append(unknownTags, tag)
		}
	}
	sort.Strings(unknownTags)
	tagOrder = append(tagOrder, unknownTags...)

	var out bytes.Buffer
	out.WriteString("# HTTP API reference\n\n")
	out.WriteString("Generated from [`api/openapi.yaml`](../api/openapi.yaml) by `make api-docs`. Do not edit manually.\n")
	for _, tag := range tagOrder {
		endpoints := endpointsByTag[tag]
		if len(endpoints) == 0 {
			continue
		}
		sort.Slice(endpoints, func(i, j int) bool {
			if methodRank(endpoints[i].method) != methodRank(endpoints[j].method) {
				return methodRank(endpoints[i].method) < methodRank(endpoints[j].method)
			}
			if endpoints[i].path != endpoints[j].path {
				return endpoints[i].path < endpoints[j].path
			}
			return endpoints[i].operationID < endpoints[j].operationID
		})
		out.WriteString("\n## ")
		out.WriteString(tag)
		out.WriteString("\n\n| METHOD | Path | operationId | Summary |\n|---|---|---|---|\n")
		for _, endpoint := range endpoints {
			fmt.Fprintf(&out, "| %s | `%s` | `%s` | %s |\n", endpoint.method, endpoint.path, endpoint.operationID, endpoint.summary)
		}
	}
	return out.Bytes(), nil
}

func cleanSummary(summary string) string {
	summary = strings.Join(strings.Fields(summary), " ")
	if summary == "" {
		return "—"
	}
	return strings.ReplaceAll(summary, "|", "\\|")
}

func methodRank(method string) int {
	for rank, known := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD", "TRACE"} {
		if method == known {
			return rank
		}
	}
	return 100
}
