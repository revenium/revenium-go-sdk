package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type provider struct {
	name       string
	module     string
	moduleDir  string
	sourceDirs []string
	typeFiles  []string
	usageRoots []string
	stopReason string
	mapperFile string
}

type surface struct {
	structs     map[string]map[string]string
	stopReasons []string
}

type report struct {
	consumed            []string
	removed             []string
	addedSiblings       []string
	stopReasonsAdded    []string
	unmappedStopReasons []string
}

const maxDepth = 3

var providers = []provider{
	{
		name:       "anthropic",
		module:     "github.com/anthropics/anthropic-sdk-go",
		moduleDir:  "anthropic",
		sourceDirs: []string{"anthropic"},
		typeFiles:  []string{"message.go"},
		usageRoots: []string{"Usage", "MessageDeltaUsage"},
		stopReason: "StopReason",
		mapperFile: "anthropic/stop_reason_mapper.go",
	},
	{
		name:       "openai",
		module:     "github.com/openai/openai-go/v3",
		moduleDir:  "openai",
		sourceDirs: []string{"openai", "perplexity"},
		typeFiles:  []string{"completion.go", "responses/response.go"},
		usageRoots: []string{"CompletionUsage", "ResponseUsage"},
	},
	{
		name:       "google",
		module:     "google.golang.org/genai",
		moduleDir:  "google",
		sourceDirs: []string{"google"},
		typeFiles:  []string{"types.go"},
		usageRoots: []string{"GenerateContentResponseUsageMetadata"},
		stopReason: "FinishReason",
		mapperFile: "google/stop_reason_mapper.go",
	},
}

var consumedPattern = regexp.MustCompile(`\b[A-Za-z_]\w*\.([A-Z][A-Za-z]*(?:\.[A-Z][A-Za-z]*)*)`)

func repoRoot() string {
	root, err := filepath.Abs(filepath.Join(".", "..", ".."))
	if err != nil {
		panic(err)
	}
	return root
}

func goFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func consumedPaths(root string, p provider) (map[string]bool, error) {
	paths := map[string]bool{}
	for _, dir := range p.sourceDirs {
		files, err := goFiles(filepath.Join(root, dir))
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			content, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			for _, match := range consumedPattern.FindAllStringSubmatch(string(content), -1) {
				for _, path := range chainSuffixes(match[1]) {
					paths[path] = true
				}
			}
		}
	}
	return paths, nil
}

func chainSuffixes(chain string) []string {
	parts := strings.Split(chain, ".")
	suffixes := make([]string, 0, len(parts))
	for i := range parts {
		suffixes = append(suffixes, strings.Join(parts[i:], "."))
	}
	return suffixes
}

func parseSurface(sources map[string]string, stopReasonType string) (surface, error) {
	structs := map[string]map[string]string{}
	var stopReasons []string
	fset := token.NewFileSet()
	for name, src := range sources {
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return surface{}, err
		}
		collectStructs(file, structs)
		stopReasons = append(stopReasons, collectTypedStringConsts(file, stopReasonType)...)
	}
	return surface{structs: structs, stopReasons: stopReasons}, nil
}

func collectStructs(file *ast.File, into map[string]map[string]string) {
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		fields := map[string]string{}
		for _, field := range structType.Fields.List {
			for _, ident := range field.Names {
				fields[ident.Name] = typeName(field.Type)
			}
		}
		into[spec.Name.Name] = fields
		return true
	})
}

func typeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.ArrayType:
		return typeName(t.Elt)
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

func collectTypedStringConsts(file *ast.File, typeNameWanted string) []string {
	var values []string
	if typeNameWanted == "" {
		return values
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || typeName(value.Type) != typeNameWanted {
				continue
			}
			for _, expr := range value.Values {
				if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					values = append(values, strings.Trim(lit.Value, `"`))
				}
			}
		}
	}
	return values
}

func flattenPaths(structs map[string]map[string]string, name string, depth int) []string {
	fields, ok := structs[name]
	if !ok || depth >= maxDepth {
		return nil
	}
	var paths []string
	for field, fieldType := range fields {
		paths = append(paths, field)
		for _, child := range flattenPaths(structs, fieldType, depth+1) {
			paths = append(paths, field+"."+child)
		}
	}
	return paths
}

func usagePaths(p provider, s surface) (map[string]bool, error) {
	paths := map[string]bool{}
	for _, root := range p.usageRoots {
		if _, ok := s.structs[root]; !ok {
			return nil, fmt.Errorf("%s: usage struct %s not found", p.module, root)
		}
		for _, path := range flattenPaths(s.structs, root, 0) {
			paths[path] = true
		}
	}
	return paths, nil
}

func mapperValues(root string, p provider) (map[string]bool, error) {
	values := map[string]bool{}
	if p.mapperFile == "" {
		return values, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, p.mapperFile), nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			values[strings.ToLower(strings.Trim(lit.Value, `"`))] = true
		}
		return true
	})
	if len(values) == 0 {
		return nil, fmt.Errorf("%s: no mapped values found", p.mapperFile)
	}
	return values, nil
}

func diffPaths(p provider, consumed map[string]bool, installed, latest surface) (report, error) {
	installedPaths, err := usagePaths(p, installed)
	if err != nil {
		return report{}, err
	}
	latestPaths, err := usagePaths(p, latest)
	if err != nil {
		return report{}, err
	}
	var r report
	for path := range consumed {
		if !installedPaths[path] {
			continue
		}
		r.consumed = append(r.consumed, path)
		if !latestPaths[path] {
			r.removed = append(r.removed, path)
		}
	}
	if len(r.consumed) == 0 {
		return report{}, fmt.Errorf("%s: no consumed path resolves against the installed structs", p.name)
	}
	for path := range latestPaths {
		if !installedPaths[path] {
			r.addedSiblings = append(r.addedSiblings, path)
		}
	}
	return r, nil
}

func diffStopReasons(p provider, installed, latest surface, mapped map[string]bool) ([]string, []string, error) {
	if p.stopReason != "" && len(latest.stopReasons) == 0 {
		return nil, nil, fmt.Errorf("%s: no %s constants found", p.module, p.stopReason)
	}
	installedReasons := map[string]bool{}
	for _, value := range installed.stopReasons {
		installedReasons[value] = true
	}
	var added, unmapped []string
	for _, value := range latest.stopReasons {
		if !installedReasons[value] {
			added = append(added, value)
		}
		if p.mapperFile != "" && !mapped[strings.ToLower(value)] {
			unmapped = append(unmapped, value)
		}
	}
	return added, unmapped, nil
}

func compare(p provider, consumed map[string]bool, installed, latest surface, mapped map[string]bool) (report, error) {
	r, err := diffPaths(p, consumed, installed, latest)
	if err != nil {
		return report{}, err
	}
	r.stopReasonsAdded, r.unmappedStopReasons, err = diffStopReasons(p, installed, latest, mapped)
	if err != nil {
		return report{}, err
	}
	for _, list := range []*[]string{&r.consumed, &r.removed, &r.addedSiblings, &r.stopReasonsAdded, &r.unmappedStopReasons} {
		sort.Strings(*list)
	}
	return r, nil
}

func (r report) failed() bool {
	return len(r.removed) > 0 || len(r.unmappedStopReasons) > 0
}

func (r report) render(p provider, installedVersion, latestVersion string) string {
	lines := []string{fmt.Sprintf("## %s: installed %s, latest %s", p.module, installedVersion, latestVersion)}
	lines = append(lines, fmt.Sprintf("consumed provider paths: %d", len(r.consumed)))
	if len(r.removed) > 0 {
		lines = append(lines, "REMOVED in latest: "+strings.Join(r.removed, ", "))
	}
	if len(r.unmappedStopReasons) > 0 {
		lines = append(lines, "UNMAPPED stop reasons: "+strings.Join(r.unmappedStopReasons, ", "))
	}
	if len(r.stopReasonsAdded) > 0 {
		lines = append(lines, "new stop reasons since installed: "+strings.Join(r.stopReasonsAdded, ", "))
	}
	if len(r.addedSiblings) > 0 {
		lines = append(lines, "new usage fields since installed: "+strings.Join(r.addedSiblings, ", "))
	}
	if len(lines) == 2 {
		lines = append(lines, "in sync")
	}
	return strings.Join(lines, "\n")
}

type moduleInfo struct {
	Version string
	Dir     string
}

func downloadModule(query string) (moduleInfo, error) {
	cmd := exec.Command("go", "mod", "download", "-json", query)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.Output()
	if err != nil {
		return moduleInfo{}, fmt.Errorf("go mod download %s: %w", query, err)
	}
	var info moduleInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return moduleInfo{}, err
	}
	if info.Dir == "" {
		return moduleInfo{}, fmt.Errorf("go mod download %s returned no directory", query)
	}
	return info, nil
}

var requirePattern = regexp.MustCompile(`(?m)^\s*(\S+) (v\S+)`)

func installedVersion(root string, p provider) (string, error) {
	content, err := os.ReadFile(filepath.Join(root, p.moduleDir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, match := range requirePattern.FindAllStringSubmatch(string(content), -1) {
		if match[1] == p.module {
			return match[2], nil
		}
	}
	return "", fmt.Errorf("%s/go.mod does not require %s", p.moduleDir, p.module)
}

func loadSources(dir string, files []string) (map[string]string, error) {
	sources := map[string]string{}
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		sources[file] = string(content)
	}
	return sources, nil
}

func run(root string) (bool, error) {
	failed := false
	for _, p := range providers {
		version, err := installedVersion(root, p)
		if err != nil {
			return false, err
		}
		installedInfo, err := downloadModule(p.module + "@" + version)
		if err != nil {
			return false, err
		}
		latestInfo, err := downloadModule(p.module + "@latest")
		if err != nil {
			return false, err
		}
		installedSources, err := loadSources(installedInfo.Dir, p.typeFiles)
		if err != nil {
			return false, err
		}
		latestSources, err := loadSources(latestInfo.Dir, p.typeFiles)
		if err != nil {
			return false, err
		}
		installed, err := parseSurface(installedSources, p.stopReason)
		if err != nil {
			return false, err
		}
		latest, err := parseSurface(latestSources, p.stopReason)
		if err != nil {
			return false, err
		}
		consumed, err := consumedPaths(root, p)
		if err != nil {
			return false, err
		}
		mapped, err := mapperValues(root, p)
		if err != nil {
			return false, err
		}
		r, err := compare(p, consumed, installed, latest, mapped)
		if err != nil {
			return false, err
		}
		fmt.Println(r.render(p, installedInfo.Version, latestInfo.Version))
		fmt.Println()
		failed = failed || r.failed()
	}
	return failed, nil
}

const fixtureInstalled = `package fixture
type Usage struct {
	InputTokens int64
	OutputTokens int64
	Details OutputTokensDetails
}
type OutputTokensDetails struct {
	ThinkingTokens int64
}
type StopReason string
const (
	StopReasonEndTurn StopReason = "end_turn"
	StopReasonMaxTokens StopReason = "max_tokens"
)
`

const fixtureLatest = `package fixture
type Usage struct {
	InputTokens int64
	OutputTokens int64
	Details OutputTokensDetails
	InferenceGeo string
}
type OutputTokensDetails struct {
	ThinkingTokens int64
}
type StopReason string
const (
	StopReasonEndTurn StopReason = "end_turn"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonRefusal StopReason = "refusal"
)
`

func selftest() (bool, error) {
	p := provider{name: "fixture", module: "fixture", usageRoots: []string{"Usage"}, stopReason: "StopReason", mapperFile: "fixture"}
	consumed := map[string]bool{"InputTokens": true, "Details.ThinkingTokens": true, "Raw": true}
	mapped := map[string]bool{"end_turn": true, "max_tokens": true}
	renamed := strings.ReplaceAll(fixtureLatest, "ThinkingTokens", "ReasoningTokens")
	compareSources := func(installedSrc, latestSrc string) (report, error) {
		installed, err := parseSurface(map[string]string{"a.go": installedSrc}, p.stopReason)
		if err != nil {
			return report{}, err
		}
		latest, err := parseSurface(map[string]string{"a.go": latestSrc}, p.stopReason)
		if err != nil {
			return report{}, err
		}
		return compare(p, consumed, installed, latest, mapped)
	}
	inSync, err := compareSources(fixtureInstalled, fixtureInstalled)
	if err != nil {
		return false, err
	}
	evolved, err := compareSources(fixtureInstalled, fixtureLatest)
	if err != nil {
		return false, err
	}
	removed, err := compareSources(fixtureInstalled, renamed)
	if err != nil {
		return false, err
	}
	_, missingRootErr := compare(provider{name: "fixture", usageRoots: []string{"Missing"}}, consumed, surface{structs: map[string]map[string]string{}}, surface{}, mapped)
	checks := []struct {
		name string
		ok   bool
	}{
		{"own-object paths are ignored", len(inSync.consumed) == 2},
		{"identical surfaces report nothing", !inSync.failed() && len(inSync.addedSiblings) == 0},
		{"new sibling field is reported", contains(evolved.addedSiblings, "InferenceGeo")},
		{"new stop reason without a mapper value fails", contains(evolved.unmappedStopReasons, "refusal") && evolved.failed()},
		{"renamed consumed field fails as removed", contains(removed.removed, "Details.ThinkingTokens") && removed.failed()},
		{"missing usage root refuses instead of reporting in sync", missingRootErr != nil},
	}
	allOk := true
	for _, check := range checks {
		status := "ok"
		if !check.ok {
			status = "FAIL"
			allOk = false
		}
		fmt.Printf("%s  %s\n", status, check.name)
	}
	if !allOk {
		return true, nil
	}
	return false, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func main() {
	var failed bool
	var err error
	if len(os.Args) > 1 && os.Args[1] == "--selftest" {
		failed, err = selftest()
	} else {
		failed, err = run(repoRoot())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider-surface-check: %v\n", err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, string(exitErr.Stderr))
		}
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}
