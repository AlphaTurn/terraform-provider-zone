terraform {
  required_providers {
    zone = {
      source  = "AlphaTurn/zone"
      version = "~> 0.1"
    }
  }
}

# Credentials are read from ZONE_USERNAME and ZONE_API_TOKEN when not set here.
# Prefer the environment: a token written into configuration ends up in version
# control and in state.
provider "zone" {}
