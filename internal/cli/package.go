package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dimetron/pi-go/internal/extension"
	"github.com/spf13/cobra"
)

func newPackageCmd() *cobra.Command {
	var project bool
	var name string

	cmd := &cobra.Command{
		Use:   "package",
		Short: "Manage shareable resource packages",
	}

	installCmd := &cobra.Command{
		Use:   "install <source>",
		Short: "Install a package from a local directory or git URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			pkg, err := extension.InstallPackage(cwd, packageScope(project), args[0], name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s package %q from %s\n", pkg.Scope, pkg.Name, pkg.Source)
			return nil
		},
	}
	installCmd.Flags().BoolVar(&project, "project", false, "Install into .pi-go/packages for the current project")
	installCmd.Flags().StringVar(&name, "name", "", "Override the installed package name")

	updateCmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an installed package from its recorded source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			pkg, err := extension.UpdatePackage(cwd, packageScope(project), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated %s package %q from %s\n", pkg.Scope, pkg.Name, pkg.Source)
			return nil
		},
	}
	updateCmd.Flags().BoolVar(&project, "project", false, "Update from .pi-go/packages in the current project")

	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			if err := extension.RemovePackage(cwd, packageScope(project), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s package %q\n", packageScope(project), args[0])
			return nil
		},
	}
	removeCmd.Flags().BoolVar(&project, "project", false, "Remove from .pi-go/packages in the current project")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			pkgs, err := extension.ListInstalledPackages(cwd)
			if err != nil {
				return err
			}
			if len(pkgs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No packages installed.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 8, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSCOPE\tSOURCE\tRESOURCES")
			for _, pkg := range pkgs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", pkg.Name, pkg.Scope, pkg.Source, summarizePackageResources(pkg.Dir))
			}
			return w.Flush()
		},
	}

	cmd.AddCommand(installCmd, updateCmd, removeCmd, listCmd)
	return cmd
}

func packageScope(project bool) extension.PackageScope {
	if project {
		return extension.PackageScopeProject
	}
	return extension.PackageScopeGlobal
}

func summarizePackageResources(dir string) string {
	var found []string
	for _, name := range []string{"extensions", "skills", "prompts", "themes", "models"} {
		if info, err := os.Stat(fmt.Sprintf("%s/%s", dir, name)); err == nil && info.IsDir() {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return "-"
	}
	return strings.Join(found, ",")
}
