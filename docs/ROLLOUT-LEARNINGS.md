# Rollout learnings

Status: static review accepted the rollout design. Native installed-upgrade verification is still pending; the current baseline tray-probe failure is being diagnosed. No successful live one-click rollout is claimed here.

## Reusable conclusions for future Uika work

- Preserve an already published baseline and issue a strictly higher version for new behavior. Replacing assets under an unchanged version cannot exercise an updater that requires a newer version and makes evidence ambiguous.
- Verify the installed program's effective version through its real running interface, then compare its bytes with the intended release executable. Finding a version-looking string inside a PE file is not proof of runtime configuration. Capturing stdout from a GUI-subsystem executable can be unreliable and can accidentally expose session URLs.
- Separate prepublication packaging proof from postpublication update proof. Before publication, run the genuine old installed application through the candidate installer and verify replacement, restart and settings preservation. After publication, exercise the old application's actual discovery, download, verification and explicit install path against live release assets. Neither test substitutes for the other.
- Keep test fixtures faithful across language boundaries. A null passed through a scripting-language string parameter can become an empty string, changing a native API's meaning. For APIs where NULL means “any” and an empty string is a filter, construct the true null inside the interop wrapper. Diagnose the fixture before weakening assertions or changing production behavior.
- Treat static acceptance and native execution as different evidence. A credible test design is permission to run the test, not evidence that installation succeeded. Preserve failures until their cause is understood and rerun the final implementation.
- A temporary directory is not sufficient isolation for installers that touch shared process names, mutexes, registry state or desktop windows. Use a disposable hosted runner, explicit environment guards and bounded execution.
- Validate release bytes after upload and before marking the release current. Preserve signer continuity across upgrades, including the first transition from unsigned to signed artifacts.

## Yozora-specific application

- Published `v0.2.1` remains the authentic baseline; PR16 targets the next patch, expected `v0.2.2`.
- The smoke installs the actual public 0.2.1 Setup and validates it against public `SHA256SUMS.txt`; it checks installed bytes against public `Yozora.exe`.
- The private `dashboard.json` identifies the running local endpoint and PID. `/api/update` supplies the effective version. The session token is sent in `X-Uika-Token` and must not appear in logs or uploaded artifacts.
- Prepublication smoke invokes the candidate Setup locally and requires the old process to exit, a new installed process to start, the intended version and executable hash, and unchanged fixture configuration. Postpublication smoke uses fresh baseline state, waits for natural discovery, and sends the real `/api/update/install` POST.
- Setup decides whether to restart using the `YozoraTrayHost` window. Its title is `Yozora tray host`. The failing PowerShell probe may have passed an empty title instead of NULL; the corrected C# wrapper uses a true null and retains the owning-PID assertion. This explanation awaits native confirmation.
- PowerShell's `Start-Process -Wait` waits for the entire process tree, including a daemon intentionally restarted by Setup. The fixture waits for Setup's own process with a 180-second limit, checks its exit code, then independently verifies the restarted daemon. A bounded wait must target the operation's actual completion boundary.
- Setup-installed users are the supported test path. Existing 0.2.1 may wait its remembered daily check interval; a fresh profile waits about two minutes plus jitter. A new manual-check button only helps after updating. Dev builds disable updates, and portable executables outside the install directory are not proven to upgrade in place.

## Pending evidence

Native Windows verification must confirm the corrected fixture, real installer handoff, version/hash and settings preservation on the final commit. Publication must then be followed by the live baseline-to-release update smoke before claiming completed operational rollout.
