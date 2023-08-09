#!/bin/bash

set -euo pipefail

echo "--- Pre install"
source .buildkite/scripts/pre-install-command.sh
add_bin_path
with_mage

echo "--- Package"
mage -v package:all

echo "--- Publich artifacts"
#   // Copy those files to another location with the sha commit to test them afterward.
#   googleStorageUpload(bucket: getBucketLocation(args.type),
#     credentialsId: "${JOB_GCS_CREDENTIALS}",
#     pathPrefix: "${BASE_DIR}/build/distributions/",
#     pattern: "${BASE_DIR}/build/distributions/**/*",
#     sharedPublicly: true,
#     showInline: true)
