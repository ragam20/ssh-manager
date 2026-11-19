package agent

import "testing"

func TestParseAgentOutput(t *testing.T) {
	out := "SSH_AUTH_SOCK=/tmp/ssh-abc/agent.123; export SSH_AUTH_SOCK;\nSSH_AGENT_PID=124; export SSH_AGENT_PID;\necho Agent pid 124;\n"
	sock, pid := ParseAgentOutput(out)
	if sock != "/tmp/ssh-abc/agent.123" || pid != "124" {
		t.Errorf("got %q %q", sock, pid)
	}
}

func TestParseAddList(t *testing.T) {
	n, fps := parseAddList("256 SHA256:aaa me@x (ED25519)\n3072 SHA256:bbb /k (RSA)\n")
	if n != 2 || !fps["SHA256:aaa"] || !fps["SHA256:bbb"] {
		t.Errorf("n=%d fps=%v", n, fps)
	}
}
