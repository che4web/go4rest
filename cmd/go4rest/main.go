package main

import (
	"fmt"
	"os"

	"github.com/che4web/go4rest/utils"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "startproject":
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: go4rest startproject <project_name>")
			os.Exit(1)
		}

		projectName := os.Args[2]
		if err := utils.CreateProject(projectName); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("project %q created\n", projectName)
	case "startapp":
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: go4rest startapp <app_name>")
			os.Exit(1)
		}

		appName := os.Args[2]
		if err := utils.CreateApp(appName); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("app %q created\n", appName)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go4rest <command> [arguments]")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  startproject <project_name>  create a new go4rest project")
	fmt.Fprintln(os.Stderr, "  startapp <app_name>  create a new go4rest app")
}
