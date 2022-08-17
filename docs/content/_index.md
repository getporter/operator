---
title: Porter Operator
description: Automate Porter on Kubernetes with the Porter Operator
layout: single
---

With Porter Operator, you define installations, credential sets and parameter sets in custom resources on a cluster, and the operator handles executing Porter when the desired state of an installation changes.
Learn more about how Porter manages desired state in our [Desired State QuickStart].

![architectural diagram showing that an installation resource triggers the operator to run a porter agent job, which then runs the bundle, saving state in mongodb](operator.png)

The initial prototype gave us a lot of feedback for how to improve Porter's support for desired state, resulting in the new [porter installation apply] command.
We are currently rewriting the operator to make use of this new command and desired state patterns.

You can watch the https://github.com/getporter/operator repository to know when new releases are ready, and participate in design discussions.

## Current State

The operator is still under development, but it is ready for you to try out and provide feedback!

[connect]: https://github.com/getporter/operator/blob/main/CONTRIBUTING.md#connect-to-the-in-cluster-mongo-database

## Next Steps

* [Porter Operator Glossary](/operator/glossary/)
* [QuickStart: Using the Porter Operator](/operator/quickstart/)
* [Porter Operator File Formats](/operator/file-formats/)

[porter installation apply]: /cli/porter_installations_apply/
[Desired State QuickStart]: /quickstart/desired-state/
