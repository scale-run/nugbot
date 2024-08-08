# Nugbot CLI

`nugbot` is a command-line tool to check for NuGet package updates in `.csproj` files. This guide will help you understand how to use this CLI app effectively.

## Prerequisites

- Access to the NuGet package repository.

## Installation

### Download Prebuilt Binaries

Prebuilt binaries for various operating systems are available thanks to GoReleaser. You can download the latest version from the [releases page](https://github.com/scale-run/nugbot/releases).

1. Download the appropriate binary for your operating system.
2. Extract the binary to a directory included in your PATH, or run it from the extracted directory.

### Example Commands

#### Check for patch updates

```sh
nugbot path/to/your/project.csproj
```

#### Check for minor updates

```sh
nugbot -u minor path/to/your/project.csproj
```

#### Check for major updates and apply them to the `.csproj` file

```sh
nugbot -u major -f path/to/your/project.csproj
```

## Options

- `-u, --update-type`: Specify the type of updates to check for (`major`, `minor`, `patch`). Default is `patch`.
- `-f, --fix`: Apply updates to the `.csproj` file.

## Logging

Logs are output in JSON format to `stderr` for easier integration with logging systems.

## How It Works

1. **Parsing**: The tool parses the `.csproj` file to extract package references.
2. **Fetching Updates**: It checks for available updates for each package from the NuGet repository.
3. **Comparing Versions**: It compares the current version with the latest available version based on the specified update type (major, minor, patch).
4. **Updating**: If the `--fix` flag is used, the `.csproj` file is updated with the new package versions.
5. **Output**: It prints the updates to `stdout` in JSON format.

## Error Handling

If any errors occur (e.g., file reading issues, parsing errors, network problems), they will be logged to `stderr` with detailed information.

## Example Azure DevOps Pipeline

```yaml
trigger:
- main

schedules:
- cron: "0 0 * * *"
  displayName: Daily midnight build
  branches:
    include:
    - main
  always: true

pool:
  vmImage: 'ubuntu-latest'

variables:
  SYSTEM_ACCESSTOKEN: $(System.AccessToken)

stages:
- stage: UpdatePackages
  jobs:
  - job: UpdatePackagesJob
    steps:
    - checkout: self

    - script: |
        # Install jq for JSON processing
        sudo apt-get install -y jq

        # Fetch the latest release information from GitHub API
        latest_release_url=$(curl -s https://api.github.com/repos/scale-run/nugbot/releases/latest | jq -r '.assets[] | select(.name | contains("nugbot-linux-amd64")) | .browser_download_url')

        # Download the latest release binary
        curl -L -o nugbot $latest_release_url

        # Make nugbot binary executable
        chmod +x ./nugbot

        # Run the nugbot tool to find updates
        ./nugbot -u patch > updates.json

        # Read the updates
        updates=$(cat updates.json | jq -c '.[]')
        for update in $updates; do
          include=$(echo $update | jq -r '.include')
          new_version=$(echo $update | jq -r '.new_version')

          # Create a new branch for each update
          branch_name="update-${include}-${new_version}"
          git checkout -b $branch_name

          # Update the package using nuget
          csproj_file=$(find . -name '*.csproj')
          nuget update $csproj_file -Id $include -Version $new_version

          # Commit and push the changes
          git add .
          git commit -m "Update $include to version $new_version"
          git push origin $branch_name

          # Create a pull request
          az repos pr create --repository $(Build.Repository.Name) \
                            --source-branch $branch_name \
                            --target-branch main \
                            --title "Update $include to version $new_version" \
                            --description "This PR updates $include to version $new_version."
        done
      displayName: 'Update Packages and Create PRs'
      env:
        AZURE_DEVOPS_EXT_PAT: $(SYSTEM_ACCESSTOKEN)
```

## Contributions

Contributions are welcome! Please fork the repository and submit pull requests.

## License

This project is licensed under the MIT License.
