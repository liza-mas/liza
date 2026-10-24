package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/mod/semver"
)

type moduleInfo struct {
	Version string `json:"Version"`
	Origin  struct {
		Hash string `json:"Hash"`
	} `json:"Origin"`
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
}

type commitInfo struct {
	SHA string `json:"sha"`
}

type compareInfo struct {
	AheadBy  int `json:"ahead_by"`
	BehindBy int `json:"behind_by"`
}

func LatestModuleVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("lookup latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lookup latest release: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read latest release response: %w", err)
	}
	return parseLatestReleaseVersion(data)
}

func MainCommit(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubMainCommitURL(), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("lookup main commit: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lookup main commit: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read main commit response: %w", err)
	}
	return parseMainCommit(data)
}

func MainCommitAhead(ctx context.Context, current, latest string) (bool, error) {
	current = normalizeCommit(current)
	latest = normalizeCommit(latest)
	if current == "" || latest == "" || sameCommit(current, latest) {
		return false, nil
	}

	url := githubCompareURL(current, latest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("compare main commits: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("compare main commits: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read compare response: %w", err)
	}
	var info compareInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return false, fmt.Errorf("parse compare response: %w", err)
	}
	return mainCommitAheadFromCompare(info), nil
}

func mainCommitAheadFromCompare(info compareInfo) bool {
	return info.AheadBy > 0 && info.BehindBy == 0
}

func parseLatestVersion(data []byte) (string, error) {
	var info moduleInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parse go list output: %w", err)
	}
	version := normalizeVersion(info.Version)
	if !semver.IsValid(version) {
		return "", fmt.Errorf("latest module version is not semantic: %q", info.Version)
	}
	return version, nil
}

func parseLatestReleaseVersion(data []byte) (string, error) {
	var info releaseInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parse latest release response: %w", err)
	}
	version := normalizeVersion(info.TagName)
	if !semver.IsValid(version) {
		return "", fmt.Errorf("latest release tag is not semantic: %q", info.TagName)
	}
	return version, nil
}

func parseMainCommit(data []byte) (string, error) {
	var commit commitInfo
	if err := json.Unmarshal(data, &commit); err == nil && commit.SHA != "" {
		sha := normalizeCommit(commit.SHA)
		if sha == "" {
			return "", fmt.Errorf("main commit response did not include sha")
		}
		return sha, nil
	}

	var info moduleInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("parse main commit response: %w", err)
	}
	sha := normalizeCommit(info.Origin.Hash)
	if sha == "" {
		return "", fmt.Errorf("main module response did not include origin hash")
	}
	return sha, nil
}

func isNewerVersion(current, latest string) bool {
	current = normalizeVersion(current)
	latest = normalizeVersion(latest)
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return false
	}
	return semver.Compare(latest, current) > 0
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "dev" || version == "unknown" {
		return ""
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func normalizeCommit(commit string) string {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if commit == "" || commit == "unknown" {
		return ""
	}
	return commit
}

func sameCommit(current, latest string) bool {
	return strings.HasPrefix(latest, current) || strings.HasPrefix(current, latest)
}

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}
