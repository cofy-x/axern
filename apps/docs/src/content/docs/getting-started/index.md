---
title: Getting Started
description: Choose the right Axern installation and client path.
---

Axern runs isolated workloads behind one public gateway. Start with Docker
Compose to evaluate the complete control plane and node runtime on one
machine, then use the same CLI context and SDK model with a Kubernetes install.

## What you need

- Docker with Compose v2
- GNU Make, curl, OpenSSL, and SSH tooling
- A Linux host, or Docker Desktop on macOS

The release quickstart downloads versioned multi-architecture images and a
checksummed CLI. It does not require Go, Rust, Python, or Node.js toolchains.

## Pick a path

| Goal | Start here |
| --- | --- |
| Evaluate Axern locally | [Compose Quickstart](/getting-started/compose/) |
| Learn product commands | [Axern CLI](/guides/cli/) |
| Build an application | [SDK overview](/sdk/) |
| Run agent evaluations | [Axrun managed rollouts](/axrun/) |
| Install on Kubernetes | [Kubernetes Install](/getting-started/kubernetes/) |

:::caution[Pre-1.0 security boundary]
The generated local credentials and loopback listeners are development
defaults. Do not reuse them for a shared deployment. Production operators own
TLS, ingress, image trust, network policy, secret storage, quotas, and durable
storage choices.
:::
