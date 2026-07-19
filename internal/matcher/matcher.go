package matcher

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"stubr/internal/config"
)

var dirConfigCache = make(map[string]*config.DirConfig)

type Match struct {
	FilePath   string
	Params     map[string]string
	MatchedDir string
	DirConfig  *config.DirConfig
}

func LoadDirConfigs(stubsDir string) error {
	absDir, err := filepath.Abs(stubsDir)
	if err != nil {
		return err
	}

	return filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		dc, err := config.LoadDirConfig(path)
		if err != nil {
			log.Printf("matcher: error loading _stubr.yaml in %s: %v", path, err)
			return nil
		}
		if dc != nil {
			dirConfigCache[path] = dc
		}
		return nil
	})
}

func MatchPath(stubsDir, method, path string) (*Match, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}

	method = strings.ToUpper(method)

	absRoot, err := filepath.Abs(stubsDir)
	if err != nil {
		return nil, err
	}

	segments := splitPath(path)
	matches := findMatches(absRoot, segments, method, 0)

	if len(matches) == 0 {
		return nil, nil
	}

	sortMatches(matches)
	best := matches[0]

	resolvedDC := resolveDirConfig(absRoot, best.matchedDir, method)

	return &Match{
		FilePath:   best.filePath,
		Params:     best.params,
		MatchedDir: best.matchedDir,
		DirConfig:  resolvedDC,
	}, nil
}

func resolveDirConfig(rootDir, matchedDir, method string) *config.DirConfig {
	rootDir = filepath.Clean(rootDir)
	matchedDir = filepath.Clean(matchedDir)

	if !strings.HasPrefix(matchedDir, rootDir) {
		return nil
	}

	rel, err := filepath.Rel(rootDir, matchedDir)
	if err != nil {
		return nil
	}

	var merged *config.DirConfig
	currentDir := rootDir

	if dc, ok := dirConfigCache[currentDir]; ok {
		merged = config.MergeDirConfigs(merged, dc)
	}

	if rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for _, part := range parts {
			currentDir = filepath.Join(currentDir, part)
			if dc, ok := dirConfigCache[currentDir]; ok {
				merged = config.MergeDirConfigs(merged, dc)
			}
		}
	}

	return config.ResolveMethodConfig(merged, method)
}

type matchResult struct {
	filePath     string
	params       map[string]string
	dynamicCount int
	matchedDir   string
}

func findMatches(baseDir string, segments []string, method string, depth int) []matchResult {
	var results []matchResult

	if depth > len(segments) {
		return results
	}

	if depth == len(segments) {
		return matchFiles(baseDir, method)
	}

	currentDir := baseDir

	seg := segments[depth]
	staticDir := filepath.Join(currentDir, seg)

	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		subResults := findMatches(staticDir, segments, method, depth+1)
		results = append(results, subResults...)
	}

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return results
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isDynamicSegment(name) || name == "_stubr.yaml" {
			continue
		}
		paramName := extractParamName(name)
		paramDir := filepath.Join(currentDir, name)
		subResults := findMatches(paramDir, segments, method, depth+1)
		for _, r := range subResults {
			if r.params == nil {
				r.params = make(map[string]string)
			}
			r.params[paramName] = seg
			r.dynamicCount++
			results = append(results, r)
		}
	}

	return results
}

func matchFiles(dir string, method string) []matchResult {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var results []matchResult

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "_stubr.yaml" {
			continue
		}
		ext := filepath.Ext(name)
		methodPart := strings.TrimSuffix(name, ext)

		if strings.EqualFold(methodPart, method) {
			results = append(results, matchResult{
				filePath:   filepath.Join(dir, name),
				matchedDir: dir,
			})
		}
	}

	if len(results) == 0 {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "_stubr.yaml" {
				continue
			}
			ext := filepath.Ext(name)
			methodPart := strings.TrimSuffix(name, ext)

			if strings.EqualFold(methodPart, "default") {
				results = append(results, matchResult{
					filePath:     filepath.Join(dir, name),
					matchedDir:   dir,
					dynamicCount: 9999,
				})
			}
		}
	}

	return results
}

func sortMatches(matches []matchResult) {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].dynamicCount != matches[j].dynamicCount {
			return matches[i].dynamicCount < matches[j].dynamicCount
		}
		return matches[i].filePath < matches[j].filePath
	})
}

func isDynamicSegment(name string) bool {
	return (strings.HasPrefix(name, ":") || strings.HasPrefix(name, "{")) &&
		(strings.HasSuffix(name, "}") || !strings.HasPrefix(name, "{"))
}

func extractParamName(name string) string {
	if strings.HasPrefix(name, ":") {
		return name[1:]
	}
	if strings.HasPrefix(name, "{") && strings.HasSuffix(name, "}") {
		return name[1 : len(name)-1]
	}
	return name
}

func splitPath(path string) []string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}

func ListAvailablePaths(stubsDir string) []string {
	var paths []string
	seen := make(map[string]bool)

	absDir, _ := filepath.Abs(stubsDir)

	filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "_stubr.yaml" {
			return nil
		}

		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return nil
		}

		ext := filepath.Ext(rel)
		nameWithoutExt := strings.TrimSuffix(rel, ext)
		dir := filepath.Dir(nameWithoutExt)
		method := filepath.Base(nameWithoutExt)

		var urlPath string
		if dir == "." {
			urlPath = "/"
		} else {
			urlPath = "/" + strings.ReplaceAll(dir, string(filepath.Separator), "/")
		}

		key := urlPath
		if !seen[key] && !strings.EqualFold(method, "default") {
			seen[key] = true
			paths = append(paths, key)
		}
		return nil
	})

	sort.Strings(paths)
	return paths
}
