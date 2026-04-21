// Package providers hosts concrete credential providers.
//
// Phase 4 scope (one subpackage per provider type):
//
//	aws_oidc, gcp_wif, azure_federated, github_app,
//	aws_secrets_manager, aws_ssm_parameter, gcp_secret_manager,
//	azure_key_vault, github_secret, vault, vault_dynamic_secret,
//	aws_profile, aws_sso, gcloud_adc, env_passthrough.
//
// Local-dev providers (aws_profile, aws_sso, gcloud_adc) refuse to run
// under CI=true. env_passthrough is flagged by lint unless
// i_understand_this_is_dangerous: true is set.
package providers
