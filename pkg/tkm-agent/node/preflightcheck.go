package node

import (
	"fmt"
	"log"

	"github.com/redhat-et/TKDK/tcv/pkg/accelerator"
	"github.com/redhat-et/TKDK/tcv/pkg/preflightcheck"
)

func RunPreflightChecks(accs map[string]accelerator.Accelerator, imageName string) error {
	log.Printf("Performing preflight checks for image %s...", imageName)
	for accType, acc := range accs {
		if err := preflightcheck.CompareTritonCacheImageToGPU(nil, acc); err != nil {
			return fmt.Errorf("accelerator %s is not compatible: %v", accType, err)
		}
	}
	return nil
}
