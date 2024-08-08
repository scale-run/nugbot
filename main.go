package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"
)

// Project struct for parsing .csproj XML
type Project struct {
	XMLName   xml.Name `xml:"Project"` //nolint:tagliatelle
	ItemGroup []struct {
		XMLName          xml.Name  `xml:"ItemGroup"`        //nolint:tagliatelle
		PackageReference []Package `xml:"PackageReference"` //nolint:tagliatelle
	} `xml:"ItemGroup"` //nolint:tagliatelle
}

// Package struct for parsing .csproj XML
type Package struct {
	Include string `xml:"Include,attr"` //nolint:tagliatelle
	Version string `xml:"Version,attr"` //nolint:tagliatelle
}

// PackageUpdate struct for storing package update info
type PackageUpdate struct {
	Include        string `json:"include,omitempty"`
	CurrentVersion string `json:"current_version,omitempty"`
	NewVersion     string `json:"new_version,omitempty"`
}

// updateType is the type of update to check for (major, minor, patch)
var (
	updateType string //nolint:gochecknoglobals
	fix        bool   //nolint:gochecknoglobals
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "nugbot <file>",
		Short: "A tool to check for nuget package updates in .csproj files",
		Args:  cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			// Setup logger
			logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
			slog.SetDefault(logger)

			filePath := args[0]
			runUpdateChecker(filePath, updateType, fix)
		},
	}

	rootCmd.Flags().StringVarP(&updateType, "update-type", "u", "patch", "Update type: major, minor, patch")
	rootCmd.Flags().BoolVarP(&fix, "fix", "f", false, "Apply updates to the .csproj file")

	if err := rootCmd.Execute(); err != nil {
		slog.Error("Command execution failed", slog.Any("error", err))
		os.Exit(1)
	}
}

// runUpdateChecker runs the update checker
func runUpdateChecker(filePath string, mmp string, fix bool) {
	file, err := os.Open(filePath)
	if err != nil {
		slog.Error("Error opening file", slog.Any("error", err))

		return
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		slog.Error("Error reading file", slog.Any("error", err))

		return
	}

	packages, err := parsePackages(filePath, bytes)
	if err != nil {
		slog.Error("Error parsing packages", slog.Any("error", err))

		return
	}

	updates := checkForUpdates(packages, mmp)

	if len(updates) > 0 {
		if fix {
			if err := updateCsprojFile(filePath, bytes, updates); err != nil {
				slog.Error("Error updating .csproj file", slog.Any("error", err))

				return
			}
		}
		writeUpdates(updates, os.Stdout)
	} else {
		slog.Info("No updates found")
	}
}

// parsePackages parses the packages from the given file
func parsePackages(filePath string, data []byte) ([]Package, error) {
	if strings.HasSuffix(filePath, ".csproj") {
		var project Project
		if err := xml.Unmarshal(data, &project); err != nil {
			return nil, fmt.Errorf("error parsing .csproj XML: %w", err)
		}
		var packages []Package
		for _, itemGroup := range project.ItemGroup {
			packages = append(packages, itemGroup.PackageReference...)
		}

		return packages, nil
	}

	return nil, errors.New("unsupported file type") //nolint:goerr113
}

// checkForUpdates checks for updates for the given packages
func checkForUpdates(packages []Package, mmp string) []PackageUpdate {
	var updates []PackageUpdate
	for _, pkg := range packages {
		latestVersion := getLatestVersion(pkg, mmp)
		if latestVersion != "" && latestVersion != pkg.Version {
			updates = append(updates, PackageUpdate{
				Include:        pkg.Include,
				CurrentVersion: pkg.Version,
				NewVersion:     latestVersion,
			})
		}
	}

	return updates
}

// getLatestVersion gets the latest version of the given package
func getLatestVersion(pkg Package, mmp string) string {
	url := fmt.Sprintf("https://api.nuget.org/v3/registration5-gz-semver1/%s/index.json", strings.ToLower(pkg.Include))
	resp, err := http.Get(url) //nolint:gosec, noctx
	if err != nil {
		slog.Error("Error fetching package info", slog.Any("error", err))

		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Error reading response", slog.Any("error", err))

		return ""
	}

	versions := gjson.GetBytes(body, "items.#.items.#.catalogEntry.version")
	if len(versions.Array()) == 0 {
		return ""
	}

	currentVersion, err := semver.NewVersion(pkg.Version)
	if err != nil {
		slog.Error("Error parsing current version", slog.Any("error", err))

		return ""
	}

	return findLatestVersion(versions.Array(), currentVersion, mmp)
}

// findLatestVersion finds the latest version from the given versions
func findLatestVersion(versions []gjson.Result, currentVersion *semver.Version, mmp string) string {
	var latestVersion *semver.Version
	for _, version := range flattenVersions(versions) {
		ver, err := semver.NewVersion(version.String())
		if err != nil || ver.Prerelease() != "" {
			continue
		}

		if ver.GreaterThan(currentVersion) {
			if isValidUpdate(currentVersion, ver, latestVersion, mmp) {
				latestVersion = ver
			}
		}
	}
	if latestVersion != nil {
		return latestVersion.String()
	}

	return ""
}

// flattenVersions flattens the versions array
func flattenVersions(versions []gjson.Result) []gjson.Result {
	var flatVersions []gjson.Result
	for _, version := range versions {
		flatVersions = append(flatVersions, version.Array()...)
	}

	return flatVersions
}

// isValidUpdate checks if the given version is a valid update
func isValidUpdate(currentVersion, ver, latestVersion *semver.Version, mmp string) bool {
	switch mmp {
	case "major":
		return latestVersion == nil || ver.GreaterThan(latestVersion)
	case "minor":
		return ver.Major() == currentVersion.Major() && (latestVersion == nil || ver.GreaterThan(latestVersion))
	case "patch":
		return ver.Major() == currentVersion.Major() && ver.Minor() == currentVersion.Minor() && (latestVersion == nil || ver.GreaterThan(latestVersion))
	}

	return false
}

// updateCsprojFile updates the .csproj file with the new versions
func updateCsprojFile(_ string, _ []byte, _ []PackageUpdate) error {
	return errors.New("not implemented") //nolint:goerr113
	// // Load the original XML
	// var project Project
	//
	//	if err := xml.Unmarshal(data, &project); err != nil {
	//		return fmt.Errorf("error parsing .csproj XML: %w", err)
	//	}
	//
	// // Create a map of updates for easy lookup
	// updateMap := make(map[string]string)
	//
	//	for _, update := range updates {
	//		updateMap[update.Include] = update.NewVersion
	//	}
	//
	// // Update the versions in the project structure
	//
	//	for i := range project.ItemGroup {
	//		for j := range project.ItemGroup[i].PackageReference {
	//			if newVersion, exists := updateMap[project.ItemGroup[i].PackageReference[j].Include]; exists {
	//				project.ItemGroup[i].PackageReference[j].Version = newVersion
	//			}
	//		}
	//	}
	//
	// // Marshal the updated project back to XML
	// output, err := xml.MarshalIndent(project, "", "  ")
	//
	//	if err != nil {
	//		return fmt.Errorf("error marshalling .csproj XML: %w", err)
	//	}
	//
	// // Write the updated XML back to the file
	//
	//	if err := os.WriteFile(filePath, output, 0644); err != nil {
	//		return fmt.Errorf("error writing .csproj file: %w", err)
	//	}
	//
	// return nil
}

// writeUpdates writes the updates to stdout
func writeUpdates(updates []PackageUpdate, w io.Writer) {
	if len(updates) > 0 {
		out, err := json.MarshalIndent(updates, "", "  ")
		if err != nil {
			slog.Error("Error marshalling updates", slog.Any("error", err))

			return
		}

		if _, err := w.Write(out); err != nil {
			slog.Error("Error writing updates", slog.Any("error", err))
		}
	}
}
