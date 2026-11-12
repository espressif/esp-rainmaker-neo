# Changelog

## 1.0.0

- Test configs are kept per deployment in `~/.espressif/rainmaker-neo/morpheus/test_config.<account-id>-<region>.json`, for a checkout and an installed copy alike, so switching deployments never mixes up their users.
- `admin test-data setup` keeps a copy in a private S3 bucket in the deployment's account (`rmng-test-config-<account-id>-<region>-an`), so a lost file is fetched back instead of regenerated with passwords the existing users do not have.
- Any command fetches a deployment's missing test config from S3 once when AWS credentials are present, and runs without it otherwise.
- Every command refuses a test config stamped for another account or region.

### Before you upgrade

- `MORPHEUS_CONFIG` is removed. Use `MORPHEUS_CONFIG_DIR` to move the folder instead.
- `cli/test_config.json` and `~/.config/morpheus/test_config.json` are no longer read. Run `esp-morpheus admin test-data setup` once per deployment to fetch or create the new file.

## 0.9.0

- Initial release: `morpheus` console client and the `esp_morpheus` SDK.
- `user`, `device`, `admin`, `app-sim` and `device-sim` command trees, each usable as a one-shot command or an interactive prompt.
- Camera streaming over KVS WebRTC ships as the optional `camera` extra: `pip install 'esp-morpheus[camera]'`.
