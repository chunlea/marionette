# Profiles

## Profiles

Profiles provide pre-configured templates for common use cases, similar to VM/VPS tiers.

```
┌────────────────────────────────────────────────────────────────────┐
│                          Profile System                            │
├────────────────────────────────────────────────────────────────────┤
│                                                                    │
│  Profile = Provider + Resources + Network + Init + Tunnels         │
│                                                                    │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                     Built-in Profiles                       │   │
│  ├─────────────────────────────────────────────────────────────┤   │
│  │  Name          │ CPU │ RAM  │ Disk │ Network    │ Special   │   │
│  ├─────────────────────────────────────────────────────────────┤   │
│  │  dev-small     │ 2   │ 4GB  │ 20GB │ allow_list │ -         │   │
│  │  dev-medium    │ 4   │ 8GB  │ 50GB │ allow_list │ -         │   │
│  │  dev-large     │ 8   │ 16GB │ 100GB│ allow_list │ -         │   │
│  │  ml-gpu        │ 8   │ 32GB │ 200GB│ allow_list │ GPU       │   │
│  │  ios-dev       │ 4   │ 16GB │ 100GB│ allow_list │ Xcode,Sim │   │
│  │  android-dev   │ 4   │ 8GB  │ 50GB │ allow_list │ Emulator  │   │
│  │  web-dev       │ 4   │ 8GB  │ 50GB │ allow_list │ Browser   │   │
│  │  secure        │ 2   │ 4GB  │ 20GB │ air_gapped │ -         │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

### Profile definition

```yaml
# configs/profiles.yaml

profiles:
  # General development profile
  - name: dev-medium
    description: "General purpose development environment"
    provider: docker-local           # Which provider to use
    resources:
      cpu: 4
      memory: 8GB
      disk: 50GB
    network:
      level: allow_list
      allowed_hosts:
        - "*.github.com"
        - "*.npmjs.org"
        - "*.pypi.org"
        - "api.anthropic.com"
        - "api.openai.com"
    labels:
      purpose: development

  # iOS development profile
  - name: ios-dev
    description: "iOS development with Xcode and Simulator"
    provider: macos-pool             # Uses pool provider
    selector:                        # Required agent labels
      os: darwin
      arch: arm64
    resources:
      cpu: 4
      memory: 16GB
      disk: 100GB
    network:
      level: allow_list
      allowed_hosts:
        - "*.apple.com"
        - "*.github.com"
        - "cocoapods.org"
        - "api.anthropic.com"
    init:
      script: |
        # Boot iOS Simulator
        xcrun simctl boot "iPhone 15 Pro" || true
        # Wait for boot
        xcrun simctl bootstatus "iPhone 15 Pro" -b
    cleanup:
      script: |
        xcrun simctl shutdown all
        rm -rf ~/Library/Developer/CoreSimulator/Caches/*
    tunnels:
      - type: ios
        auto: true                   # Auto-create tunnel for simulator
      - type: desktop
        auto: true                   # Also stream macOS desktop
    labels:
      purpose: ios-development
      requires: xcode

  # Android development profile
  - name: android-dev
    description: "Android development with emulator"
    provider: docker-local
    image: marionette/agent-android:latest
    resources:
      cpu: 4
      memory: 8GB
      disk: 50GB
    network:
      level: allow_list
      allowed_hosts:
        - "*.android.com"
        - "*.google.com"
        - "*.github.com"
        - "maven.google.com"
    init:
      script: |
        # Start emulator
        emulator -avd Pixel_7_API_34 -no-window -no-audio &
        adb wait-for-device
    tunnels:
      - type: android
        auto: true
    labels:
      purpose: android-development

  # Secure/air-gapped profile
  - name: secure
    description: "Air-gapped environment for sensitive tasks"
    provider: e2b-cloud
    resources:
      cpu: 2
      memory: 4GB
      disk: 20GB
    network:
      level: air_gapped              # No internet access
    labels:
      purpose: secure
      compliance: high
```

### Profile CLI usage

```bash
# List available profiles
mctl profiles list

# Show profile details
mctl profiles describe ios-dev

# Create runner with profile (admin)
mctl admin runners spawn --profile ios-dev --name "ios-runner-1"

# Override profile settings
mctl admin runners spawn \
  --profile dev-medium \
  --memory 12GB \
  --name "custom-runner"

# Create task requiring specific profile
mctl tasks create \
  --profile ios-dev \
  --agent claude \
  --prompt "Build an iOS weather app with SwiftUI"

# Task inherits profile's network, init scripts, and tunnels
```
