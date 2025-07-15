package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	pb "github.com/redhat-et/GKM/pkg/gkm-csi-plugin/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	var image, crdName, addr, namespace string
	var loadFlag, unloadFlag bool

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	flag.BoolVar(&loadFlag, "load", false,
		"Download and unpack and OCI Image containing a GPU Kernel Cache and\n"+
			"load the cached kernel on the local filesystem.\n")

	flag.BoolVar(&unloadFlag, "unload", false,
		"Remove an already downloaded OCI Image containing a GPU Kernel Cache\n"+
			"from the local filesystem\n")

	flag.StringVar(&image, "image", "",
		"Image repository URL of OCI Image with GPU Kernel Cache.\n"+
			"Example: -image quay.io/repository/mtahhan/01-vector-add-cache:latest\n")

	flag.StringVar(&crdName, "crdName", "",
		"Name of the GPU Kernel Cache CRD instance. If testing in a non-Kubernetes\n"+
			"environment, this is just a tag. Same value from \"csi.gkm.io/GKMCache\"\n"+
			"in the PodSpec Volume.\n"+
			"Example: -crdName kernel-x\n")

	flag.StringVar(&namespace, "namespace", "",
		"Namespace of the GPU Kernel Cache CRD instance. Leave blank for cluster scoped CRDs.\n"+
			"If testing in a non-Kubernetes environment, this is just a tag.\n"+
			"Example: -namespace my-app-ns\n")

	flag.StringVar(&addr, "addr", "localhost:50051",
		"Address to connect to. Defaults to \"localhost:50051\"\n"+
			"Example: -addr localhost:52000\n")

	flag.Parse()

	if loadFlag && unloadFlag {
		log.Printf("Only \"load\" or \"unload\" can be entered on a given command.\n")
		return
	} else if !loadFlag && !unloadFlag {
		log.Printf("One of \"load\" or \"unload\" is required.\n")
		return
	}

	if loadFlag {
		if image == "" {
			log.Printf("For \"load\", \"-image\" is required.\n")
			return
		}
		if crdName == "" {
			log.Printf("For \"load\", \"-crdName\" is required.\n")
			return
		}
	}

	if unloadFlag {
		if crdName == "" {
			log.Printf("For \"unload\", \"-crdName\" is required.\n")
			return
		}
	}

	// Set up a connection to the server.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Fatalf("error closing connection: %v", err)
		}
	}()

	c := pb.NewGkmCsiServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Call Image Sever based on input data
	if loadFlag {
		loadReq := pb.LoadKernelImageRequest{
			Image: &pb.ImageLocation{
				Url: image,
			},
			Name: crdName,
		}

		if namespace != "" {
			loadReq.Namespace = &namespace
		}

		r, err := c.LoadKernelImage(ctx, &loadReq)
		if err != nil {
			log.Fatalf("error calling function LoadKernelImage: %v", err)
		}

		log.Printf("Response from gRPC server's LoadKernelImage function: %s", r.GetMessage())
	} else if unloadFlag {
		unloadReq := pb.UnloadKernelImageRequest{
			Name: crdName,
		}

		if namespace != "" {
			unloadReq.Namespace = &namespace
		}

		r, err := c.UnloadKernelImage(ctx, &unloadReq)
		if err != nil {
			log.Fatalf("error calling function UnloadKernelImage: %v", err)
		}

		log.Printf("Response from gRPC server's UnloadKernelImage function: %s", r.GetMessage())
	}
}
