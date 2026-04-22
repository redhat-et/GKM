#!/bin/bash

CALL_POPD=false
if [[ "$PWD" != */examples ]]; then
    pushd examples &>/dev/null
    if [[ $? -ne 0 ]]; then
        echo "ERROR: Must run from \"./GKM\" or \"./GKM/examples\""
        exit 1
    fi
    CALL_POPD=true
fi

rm -f base/common/namespace-1.yaml
rm -f base/scope/cluster/namespace-2.yaml
rm -f overlays/access/*.yaml
rm -f overlays/scope/*.yaml
rm -f output/*.yaml
rm -f variants/access/rox/*.yaml
rm -f variants/access/rwo/*.yaml
rm -f variants/scope/cluster/*.yaml
rm -f variants/scope/namespace/*.yaml

if [[ "$CALL_POPD" == true ]]; then
    popd &>/dev/null || exit
fi
