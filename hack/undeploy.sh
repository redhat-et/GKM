#!/usr/bin/env bash

# Control script behavior
DEBUG=${DEBUG:-false}

# Process input flags
FORCE_FLAG=false

case "$1" in
  "force"|"-force"|"--force")
    FORCE_FLAG=true
    ;;
  *)
    if [ "$1" != "" ]; then
      echo "Unknown input: $1"
      exit 1
    fi
    ;;
esac

# Test for kubectl or oc
kubectl version  &>/dev/null
if [ $? != 0 ]; then
  oc version  &>/dev/null
  if [ $? != 0 ]; then
    echo "ERROR: Either \`kubectl\` or \`oc\` must be installed. Exiting ..."
    echo
    exit 1
  fi

  alias kubectl="oc"

  if [[ "$DEBUG" == true ]]; then
    echo "oc detected"
  fi
else
  if [[ "$DEBUG" == true ]]; then
    echo "kubectl detected"
  fi
fi

GKM_CACHE_OUTPUT=$(kubectl get gkmcaches -A --no-headers -o custom-columns=NAMESPACE:.metadata.namespace)
if [[ $? == 0 ]] && [[ "$GKM_CACHE_OUTPUT" != "" ]]; then
  if [[ "$DEBUG" == true ]]; then
    echo "GKMCache instance Exist!"
    echo "${GKM_CACHE_OUTPUT}"
  fi

  # If no force flag, then exit because GKMCache instances still exist.
  if [[ "$FORCE_FLAG" == false ]]; then
    echo
    while IFS= read -r line; do
      echo "Need to delete GKMCache \"$line\" and any pods using the associated cache."
    done <<< "$GKM_CACHE_OUTPUT"

    echo
    echo "GKMCache instances still exist. Cannot undeploy GKM. Remove all GKMCache and ClusterGKMCache"
    echo "instances and retry, or run the force version of the undeploy command."
    echo
    echo "Aborting undeploy request."
    echo
    exit 1
  else
    # Force flag is set. Because workload is contained in a Namespace, delete any workload namespaces.
    echo "Attempt to remove workload namespaces:"

    while IFS= read -r line; do
      if [[ "$line" == "default" ]]; then
        echo "Can't handle namespace \"${line}\" yet, exiting"
        exit 1
      else
        echo
        echo "Deleting namespace \"$line\":"
        kubectl delete namespace ${line}
        if [ $? == 0 ]; then
          echo "Deleting namespace \"$line\" was successful."
        else
          echo "Deleting namespace \"$line\" was NOT successful."
          exit 1
        fi
      fi
    done <<< "$GKM_CACHE_OUTPUT"
    echo
  fi
else
  if [[ "$DEBUG" == true ]]; then
    echo "NO GKMCache instance Exist!"
  fi
fi

CLUSTER_GKM_CACHE_OUTPUT=$(kubectl get clustergkmcaches -A --no-headers -o custom-columns=NAME:.metadata.name)
if [[ $? == 0 ]] && [[ "$CLUSTER_GKM_CACHE_OUTPUT" != "" ]]; then
  if [[ "$DEBUG" == true ]]; then
    echo "Cluster GKM Cache instance Exist!"
    echo "${CLUSTER_GKM_CACHE_OUTPUT}"
  fi

  # If no force flag, then exit because ClusterGKMCache instances still exist.
  if [[ "$FORCE_FLAG" == false ]]; then
    echo
    while IFS= read -r line; do
      echo "Need to delete ClusterGKMCache \"$line\" and any pods using the associated cache."
    done <<< "$CLUSTER_GKM_CACHE_OUTPUT"

    echo
    echo "ClusterGKMCache instances still exist. Cannot undeploy GKM. Remove all GKMCache and ClusterGKMCache"
    echo "instances and retry, or run the force version of the undeploy command."
    echo
    echo "Aborting undeploy request."
    echo
    exit 1
  else
    echo
    while IFS= read -r line; do
      echo "Need to delete ClusterGKMCache \"$line\" and any pods using the associated cache."
    done <<< "$CLUSTER_GKM_CACHE_OUTPUT"

    echo
    echo "Can't handle ClusterGKMCache cleanup yet, exiting"
    echo
    exit 1
  fi
else
  if [[ "$DEBUG" == true ]]; then
    echo "NO ClusterGKMCache instance Exist!"
  fi
fi

echo "GKM can now be safely uninstalled."
exit 0
