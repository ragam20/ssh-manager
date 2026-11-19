package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"sshm/internal/agent"
	"sshm/internal/store"
	"sshm/internal/tunnel"
)

func tunnelCommand(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	const usage = "usage: sshm tunnel list | sshm tunnel (start|stop) <name|id>"
	list := len(args) == 1 && args[0] == "list"
	if !list && (len(args) != 2 || (args[0] != "start" && args[0] != "stop")) {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	st, err := store.Load(path)
	if err != nil {
		return fail(stderr, fmt.Errorf("read tunnel definitions: %w", err))
	}
	if list {
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tID\tSTATUS")
		for _, spec := range st.Tunnels {
			status := "stopped"
			if tunnel.Running(spec) {
				status = "running"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", spec.Name, spec.ID, status)
		}
		return fail(stderr, tw.Flush())
	}
	spec, err := findTunnel(st.Tunnels, args[1])
	if err != nil {
		return fail(stderr, err)
	}
	if err := spec.Validate(); err != nil {
		return fail(stderr, fmt.Errorf("tunnel %s: %w", spec.ID, err))
	}
	if args[0] == "stop" {
		if err := tunnel.Stop(spec); err != nil {
			return fail(stderr, fmt.Errorf("stop tunnel %s: %w", spec.ID, err))
		}
		fmt.Fprintf(stdout, "Stopped %s (%s)\n", spec.Name, spec.ID)
		return 0
	}
	if tunnel.Running(spec) {
		fmt.Fprintf(stdout, "%s (%s) is already running\n", spec.Name, spec.ID)
		return 0
	}
	if err := agent.Ensure(); err != nil {
		return fail(stderr, err)
	}
	cmd := tunnel.StartCmd(spec)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return fail(stderr, fmt.Errorf("start tunnel %s: %w", spec.ID, err))
	}
	fmt.Fprintf(stdout, "Started %s (%s)\n", spec.Name, spec.ID)
	return 0
}

func findTunnel(specs []tunnel.Spec, selector string) (tunnel.Spec, error) {
	for _, spec := range specs {
		if spec.ID == selector {
			return spec, nil
		}
	}
	var match *tunnel.Spec
	for i := range specs {
		if specs[i].Name != selector {
			continue
		}
		if match != nil {
			return tunnel.Spec{}, fmt.Errorf("multiple tunnels named %q. Use an ID from `sshm tunnel list`", selector)
		}
		match = &specs[i]
	}
	if match == nil {
		return tunnel.Spec{}, fmt.Errorf("tunnel %q not found", selector)
	}
	return *match, nil
}
