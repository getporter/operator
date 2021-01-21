#!/usr/bin/env sh

set -euo pipefail

# Copy user-defined porter configuration into PORTER_HOME
echo "loading porter configuration..."
cp -L /porter-config/config.* /root/.porter/
ls | grep config.*
cat /root/.porter/config.*

# Print the version of porter we are using for this run
porter version

# Execute the command passed
echo "porter $@"
porter $@
