# Security Policy

## Supported versions

Security fixes are provided for the latest released Agentmetry version. Update
to the newest release before reporting an issue that may already be resolved.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability or include sensitive
telemetry in a public report. Use GitHub's private vulnerability reporting for
the repository:

[Report a vulnerability privately](https://github.com/theoden9014/agentmetry/security/advisories/new)

Include the affected Agentmetry version, operating system, deployment profile,
reproduction steps, security impact, and a minimal sanitized example. Do not
attach real prompts, credentials, telemetry databases, or command output.

## Local trust boundary

Agentmetry binds its supported default listeners to loopback and stores data on
the local machine. It does not provide authentication for exposing those
listeners to a LAN or the public internet. Such exposure is outside the
supported configuration.

Telemetry can contain prompts, responses, tool inputs and outputs, file paths,
and secrets printed by commands. Enable source-side content logging only when
appropriate, protect the local database with operating-system access controls,
and avoid sharing unsanitized databases or fixtures.

Signed desktop updates verify updater artifacts before installation. Download
desktop packages from the repository's GitHub Releases page and keep automatic
updates enabled.
