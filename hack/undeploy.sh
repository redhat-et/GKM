#!/usr/bin/env bash

# Control script behavior
DEBUG=${DEBUG:-false}
WORK=${WORK:-true}

ABORT_FLAG=false

# Process input flags
FORCE_FLAG=false

verify_kubectl() {
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
}

abort_msg_and_exit() {
  echo
  echo "GKMCache and/or ClusterGKMCache instances still exist. Cannot undeploy GKM."
  echo "Remove all GKMCache and ClusterGKMCache instances along with pods using the cache"
  echo "and retry, or run the force version of the undeploy command."
  echo
  echo "Aborting undeploy request."
  echo
  exit 1
}

# Determine if any GKMCache instances exist and cleanup if force is set.
process_gkm_cache() {
  if [[ "$DEBUG" == true ]]; then
    echo "Process GKMCache Instances"
  fi

  # Retrieve the Namespaces the GKMCaches are created in.
  GKM_CACHE_NAMESPACE_OUTPUT=$(kubectl get gkmcaches -A --no-headers -o custom-columns=NAMESPACE:.metadata.namespace)
  if [[ $? == 0 ]] && [[ "$GKM_CACHE_NAMESPACE_OUTPUT" != "" ]]; then
    if [[ "$DEBUG" == true ]]; then
      echo "GKMCache instance Exist!"
      echo "${GKM_CACHE_NAMESPACE_OUTPUT}"
    fi

    if [[ "$FORCE_FLAG" == false ]]; then
      # Force flag is not set and GKMCache instances exist. Print them and abort.
      echo
      while IFS= read -r GKM_CACHE_NAMESPACE; do
        echo "Need to delete GKMCache \"$GKM_CACHE_NAMESPACE\" and any pods using the associated cache."
      done <<< "$GKM_CACHE_NAMESPACE_OUTPUT"

      # Since force flag is not set, skip the abort, but remember via flag. This will let
      # ClusterGKMCache print any instances to help user with cleanup.
      ABORT_FLAG=true
    else
      # Force flag is set. Because workload is contained in a Namespace, delete any workload namespaces.
      echo "Attempt to remove GKMCache workload namespaces:"

      # Loop through GKMCache Namespaces and attempt to delete Namespace.
      while IFS= read -r GKM_CACHE_NAMESPACE; do
        if [[ "$GKM_CACHE_NAMESPACE" == "default" ]]; then
          echo "Can't handle namespace \"${GKM_CACHE_NAMESPACE}\" yet, exiting"
          abort_msg_and_exit
        else
          echo
          echo "Deleting namespace \"$GKM_CACHE_NAMESPACE\":"
          if [[ "$WORK" == true ]]; then
            kubectl delete namespace --ignore-not-found=true ${GKM_CACHE_NAMESPACE}
          fi
          if [ $? == 0 ]; then
            echo "Deleting namespace \"$GKM_CACHE_NAMESPACE\" was successful."
          else
            echo "Deleting namespace \"$GKM_CACHE_NAMESPACE\" was NOT successful."
            abort_msg_and_exit
          fi
        fi
      done <<< "$GKM_CACHE_NAMESPACE_OUTPUT"

      if [[ "$WORK" == true ]]; then
        # All GKMCache Namespaces should have been deleted. Verify all GKMCache are deleted.
        # Retrieve the names of all GKMCaches.
        GKM_CACHE_NAME_OUTPUT=$(kubectl get gkmcaches -A --no-headers -o custom-columns=NAME:.metadata.name)
        if [[ $? == 0 ]] && [[ "$GKM_CACHE_NAME_OUTPUT" != "" ]]; then
          echo
          echo "GKMCache instances still exist after Namespace deletion!"
          echo "${GKM_CACHE_NAME_OUTPUT}"

          abort_msg_and_exit
        else
          echo
          echo "GKMCache cleanup complete."
        fi
      else
        echo
        echo "GKMCache cleanup complete."
      fi
    fi
  else
    if [[ "$DEBUG" == true ]]; then
      echo "NO GKMCache instance Exist!"
    fi
  fi
}

# Determine if any ClusterGKMCache instances exist and cleanup if force is set.
process_cluster_gkm_cache() {
  if [[ "$DEBUG" == true ]]; then
    echo "Process ClusterGKMCache Instances"
  fi

  # Retrieve the Name of any ClusterGKMCaches that are created.
  CLUSTER_GKM_CACHE_NAME_OUTPUT=$(kubectl get clustergkmcaches --no-headers -o custom-columns=NAME:.metadata.name)
  if [[ $? == 0 ]] && [[ "$CLUSTER_GKM_CACHE_NAME_OUTPUT" != "" ]]; then
    if [[ "$DEBUG" == true ]]; then
      echo "Cluster GKM Cache instances Exist!"
      echo "${CLUSTER_GKM_CACHE_NAME_OUTPUT}"
    fi

    if [[ "$FORCE_FLAG" == false ]]; then
      # Force flag is not set and ClusterGKMCache instances exist. Print them and abort.
      echo
      while IFS= read -r CLUSTER_GKM_CACHE_NAME; do
        echo "Need to delete ClusterGKMCache \"$CLUSTER_GKM_CACHE_NAME\" and any pods using the associated cache."
      done <<< "$CLUSTER_GKM_CACHE_NAME_OUTPUT"

      # Since force flag is not set, skip the abort, but remember via flag. This will let
      # GKMCache print any instances to help user with cleanup.
      ABORT_FLAG=true
    else
      # Force flag is set, the determine if any ClusterGKMCacheNode instances exist.
      # This is where pod usages is returned and will be used in the cleanup.
      CLUSTER_GKM_CACHE_NODE_OUTPUT=$(kubectl get clustergkmcachenodes --no-headers -o custom-columns=NAME:.metadata.name)
      if [[ $? == 0 ]] && [[ "$CLUSTER_GKM_CACHE_NODE_OUTPUT" != "" ]]; then
        if [[ "$DEBUG" == true ]]; then
          echo "Cluster GKM Cache Node instances Exist!"
          echo "${CLUSTER_GKM_CACHE_NODE_OUTPUT}"
        fi

        # Because workload is contained in a Namespace, loop through workload Namespaces
        # and attempt to delete Namespace.
        echo
        echo "Attempt to remove ClusterGKMCache workload namespaces:"

        while IFS= read -r CLUSTER_GKM_CACHE_NODE; do
          POD_NAMESPACE_OUTPUT=$(kubectl get clustergkmcachenode $CLUSTER_GKM_CACHE_NODE -o yaml | grep podNamespace | awk -F' ' '{print $2}')
          if [[ $? == 0 ]] && [[ "$POD_NAMESPACE_OUTPUT" != "" ]]; then

            if [[ "$DEBUG" == true ]]; then
              echo "Pod namespace instances Exist!"
              echo "${POD_NAMESPACE_OUTPUT}"
            fi

            while IFS= read -r POD_NAMESPACE; do
              if [[ "$POD_NAMESPACE" == "default" ]]; then
                echo "Can't handle namespace \"${POD_NAMESPACE}\" yet, exiting"
                exit 1
              else
                echo
                echo "Delete namespace \"$POD_NAMESPACE\" to remove any pods using the associated cluster cache:"
                if [[ "$WORK" == true ]]; then
                  kubectl delete namespace --ignore-not-found=true ${POD_NAMESPACE}
                fi
                if [ $? == 0 ]; then
                  echo "Deleting namespace \"$POD_NAMESPACE\" was successful."
                else
                  echo "Deleting namespace \"$POD_NAMESPACE\" was NOT successful."
                  abort_msg_and_exit
                fi
              fi
            done <<< "$POD_NAMESPACE_OUTPUT"

          else
            if [[ "$DEBUG" == true ]]; then
              echo
              echo "NO Pods exist in ClusterGKMCache  \"$CLUSTER_GKM_CACHE_NODE\" instance."
            fi
          fi
        done <<< "$CLUSTER_GKM_CACHE_NODE_OUTPUT"
      else
        if [[ "$DEBUG" == true ]]; then
          echo
          echo "NO ClusterGKMCacheNodes instance Exist!"
        fi
      fi

      # Now that workload pods are cleaned up, attempt to delete ClusterGKMCache instances.
      while IFS= read -r CLUSTER_GKM_CACHE_NAME; do
        echo
        echo "Deleting ClusterGKMCache \"$CLUSTER_GKM_CACHE_NAME\":"
        if [[ "$WORK" == true ]]; then
          kubectl delete clustergkmcache ${CLUSTER_GKM_CACHE_NAME}
        fi
        if [ $? == 0 ]; then
          echo "Deleting ClusterGKMCache \"$CLUSTER_GKM_CACHE_NAME\" was successful."
        else
          echo "Deleting ClusterGKMCache \"$CLUSTER_GKM_CACHE_NAME\" was NOT successful."
          abort_msg_and_exit
        fi
      done <<< "$CLUSTER_GKM_CACHE_NAME_OUTPUT"

      if [[ "$WORK" == true ]]; then
        # All ClusterGKMCache instances should have been deleted. Verify they all are deleted.
        # Retrieve the names of all ClusterGKMCaches.
        CLUSTER_GKM_CACHE_NAME_OUTPUT=$(kubectl get clustergkmcaches --no-headers -o custom-columns=NAME:.metadata.name)
        if [[ $? == 0 ]] && [[ "$CLUSTER_GKM_CACHE_NAME_OUTPUT" != "" ]]; then
          echo
          echo "ClusterGKMCache instances still exist after deletion attempt!"
          echo "${CLUSTER_GKM_CACHE_NAME_OUTPUT}"

          abort_msg_and_exit
        else
          echo
          echo "ClusterGKMCache cleanup complete."
        fi
      else
        echo
        echo "ClusterGKMCache cleanup complete."
      fi
    fi
  else
    if [[ "$DEBUG" == true ]]; then
      echo
      echo "NO ClusterGKMCache instance Exist!"
    fi
  fi

  # Determine if any ClusterGKMCache instances exist.
  CLUSTER_GKM_CACHE_OUTPUT=$(kubectl get clustergkmcaches --no-headers -o custom-columns=NAME:.metadata.name)
  if [[ $? == 0 ]] && [[ "$CLUSTER_GKM_CACHE_OUTPUT" != "" ]]; then
    if [[ "$DEBUG" == true ]]; then
      echo
      echo "Cluster GKM Cache instances Exist!"
      echo "${CLUSTER_GKM_CACHE_OUTPUT}"
    fi
  fi
}

#
# Main
#
verify_kubectl

# Process Input Variables
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

process_gkm_cache
echo
process_cluster_gkm_cache
echo

if [[ "$ABORT_FLAG" == true ]]; then
  abort_msg_and_exit
fi

echo
echo "GKM can now be safely uninstalled."
echo

exit 0
