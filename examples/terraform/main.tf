terraform {
  required_version = ">= 1.3"
  required_providers {
    flagctl = {
      source = "ahmedr1zwan/flagctl"
    }
  }
}

# Uses FLAGCTL_SERVER, or http://127.0.0.1:8080 when unset.
# This provider is built locally; follow docs/terraform.md before running it.
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
