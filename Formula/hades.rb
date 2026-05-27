# frozen_string_literal: true

#
# Formula/hades.rb — local mirror for verify-brew-formula self-check.
#
# Canonical authority: github.com/cbip-solutions/homebrew-tap/Formula/hades.rb.
# This local mirror is kept in sync by the verify-brew-formula gate and the
# GoReleaser brews block.
#
class Hades < Formula
  desc "Autonomous agentic development orchestrator (Hermes substrate)"
  homepage "https://github.com/cbip-solutions/hades-system"
  url "https://github.com/cbip-solutions/hades-system/releases/download/v#{version}/hades-#{version}-darwin-arm64.tar.gz"
  version "1.0.0"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "MIT"

  depends_on "go@1.25" => :build
  depends_on "hermes-agent" => :required # MIT; mandatory substrate (ADR-0080)

  def install
    bin.install "bin/hades"
    bin.install "bin/zen-swarm-ctld"
    pkgshare.install "share/migrations" if (buildpath/"share/migrations").exist?
    (etc/"hades").mkpath
  end

  def caveats
    <<~EOS
      HADES requires Hermes Agent (MIT) as its substrate. Hermes Agent is
      installed automatically as a required dependency.
      Verify: `hades doctor hermes`

      HADES uses Caronte (in-tree code-graph; Apache-2.0/MIT per project
      LICENSE) — no external code-graph dependency required.

      Default LLM access: Anthropic API + Gemini + OpenRouter via the the release design
      provider cascade (paygo). Configure with:
        `hades providers add anthropic --key $ANTHROPIC_API_KEY`
        `hades providers add gemini --key $GEMINI_API_KEY`
        `hades providers add openrouter --key $OPENROUTER_API_KEY`

      Optional advanced configuration: a private Tier 1 sidecar can be
      attached for direct Anthropic Max subscription integration. See
      public Tier 1 sidecar recipe for the HTTP API contract; the reference
      implementation is distributed separately.

      License: MIT (see LICENSE). MIT permits commercial use; no commercial
      license required for any HADES component.
    EOS
  end

  service do
    run [opt_bin/"zen-swarm-ctld"]
    keep_alive true
    log_path var/"log/zen-swarm-ctld.log"
    error_log_path var/"log/zen-swarm-ctld.error.log"
  end

  test do
    system "#{bin}/hades", "--version"
    system "#{bin}/hades", "doctor", "--json"
  end
end
