# Cut a Release

🧀💨

Our CI system watches for tags, and when a tag is pushed, cuts a release
of Porter. When you are asked to cut a new release, here is the process:

1. Figure out the correct version number using our [version strategy].
    * Bump the major segment if there are any breaking changes, and the 
      version is greater than v1.0.0
    * Bump the minor segment if there are new features only.
    * Bump the patch segment if there are bug fixes only.
    * Bump the pre-release number (version-prerelease.NUMBER) if this is
      a pre-release, e.g. alpha/beta/rc.
1. First, ensure that the main CI build has already passed for 
    the [commit that you want to tag][commits], and has published the canary binaries. 
    
    Then create the tag and push it:

    ```
    git checkout main
    git pull
    git tag VERSION -a -m ""
    git push --tags
    ```
    If the CI build failed to build for the release, fix the problem first. 
    Then increment the PATCH version, e.g. v0.7.0->v0.7.1, and go through the above steps again to publish the binaries. 
    It's often a good pratice to finish the release first before updating any of our docs that references the latest release.

1. Generate some release notes and put them into the release on GitHub.
   - Go to Operator Github repository and find the newly created release tag. You should see a
   "auto generate release notes" button to create release notes for the release.
   - Modify the generated release note to call out any breaking or notable changes in the release.
   - Include instructions for installing or upgrading to the new release:
    ```
      # Install or Upgrade
      Run (or re-run) the installation from https://getporter.org/operator/install/ to get the
    latest version of operator.
    ```
1. Announce the new release in the community.
   - Email the [mailing list](https://getporter.org/mailing-list) to announce the release. In your email, call out any breaking or notable changes.
   - Post a message in [Porter's slack channel](https://getporter.org/community/#slack).
1. If there are any issues fixed in the release and someone is waiting for the fix, comment on the issue to let them know and link to the release.
1. If the release contains new features, it should be announced through a [blog](https://getporter.org/blog/) post and on Porter's twitter account.

[maintainers]: https://github.com/orgs/getporter/teams/maintainers
[admins]: https://github.com/orgs/getporter/teams/admins
[commits]: https://github.com/getporter/porter/commits/main
[version strategy]: https://getporter.org/project/version-strategy/
