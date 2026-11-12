- source_spec: `spec-cancel-command-start.md`
  summary: Verify real Docker behavior when SIGINT races with an in-flight container start.
  evidence: Unverified: Docker may reject SIGINT before a starting container is running; forced removal remains the fallback. A controlled real-engine test is needed to establish the interleaving and impact.
