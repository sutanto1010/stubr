package matcher

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Match struct {
	FilePath string
	Params   map[string]string
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

	segments := splitPath(path)
	matches := findMatches(stubsDir, segments, method, 0)

	if len(matches) == 0 {
		return nil, nil
	}

	sortMatches(matches)
	best := matches[0]

	return &Match{
		FilePath: best.filePath,
		Params:   best.params,
	}, nil
}

type matchResult struct {
	filePath     string
	params       map[string]string
	dynamicCount int
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
		for _, r := range subResults {
			r.dynamicCount += 0
			results = append(results, r)
		}
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
		if !isDynamicSegment(name) {
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
		ext := filepath.Ext(name)
		methodPart := strings.TrimSuffix(name, ext)

		if strings.EqualFold(methodPart, method) {
			results = append(results, matchResult{
				filePath: filepath.Join(dir, name),
			})
		}
	}

	if len(results) == 0 {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			ext := filepath.Ext(name)
			methodPart := strings.TrimSuffix(name, ext)

			if strings.EqualFold(methodPart, "default") {
				results = append(results, matchResult{
					filePath:     filepath.Join(dir, name),
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

	filepath.WalkDir(stubsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(stubsDir, path)
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
