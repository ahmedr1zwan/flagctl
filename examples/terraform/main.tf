terraform {
  required_version = ">= 1.3"
  required_providers {
    flagctl = {
      source = "ahmedr1zwan/flagctl"
    }
  }
}

# Uses FLAGCTL_SERVER, or http://127.0.0.1:8080 when unset.
# Install via docs/releases.md (binary mirror) or docs/terraform.md (source build).
provider "flagctl" {}

resource "flagctl_flag" "checkout" {
  environment = "dev"
  key         = "terraform_checkout"
  description = "Checkout managed by Terraform"
  enabled     = true
}

output "checkout" {
  value = flagctl_flag.checkout
}
