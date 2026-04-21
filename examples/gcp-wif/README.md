# gcp-wif

Single-cloud GCP using Workload Identity Federation. GitHub Actions OIDC
→ GCP STS → service account impersonation.

## One-time cloud setup

```bash
PROJECT_ID=mycompany-prod
PROJECT_NUMBER=$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')
REPO=myorg/myrepo

# Pool
gcloud iam workload-identity-pools create github \
  --project=$PROJECT_ID --location=global \
  --display-name="GitHub Actions"

# Provider (pinned to your repo)
gcloud iam workload-identity-pools providers create-oidc reeve \
  --project=$PROJECT_ID \
  --workload-identity-pool=github --location=global \
  --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
  --attribute-condition="assertion.repository=='$REPO'"

# Service account for reeve
gcloud iam service-accounts create reeve-prod \
  --project=$PROJECT_ID \
  --display-name="reeve GitOps"

# Read-only SA for drift
gcloud iam service-accounts create reeve-drift-readonly \
  --project=$PROJECT_ID \
  --display-name="reeve drift (read-only)"

# Allow the WIF pool to impersonate both SAs
for sa in reeve-prod reeve-drift-readonly; do
  gcloud iam service-accounts add-iam-policy-binding \
    $sa@$PROJECT_ID.iam.gserviceaccount.com \
    --role=roles/iam.workloadIdentityUser \
    --member="principalSet://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github/attribute.repository/$REPO"
done

# Grant whatever project roles the stacks need (scope to least-privilege)
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:reeve-prod@$PROJECT_ID.iam.gserviceaccount.com" \
  --role=roles/editor

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:reeve-drift-readonly@$PROJECT_ID.iam.gserviceaccount.com" \
  --role=roles/viewer
```

## GCS bucket

```bash
gcloud storage buckets create gs://mycompany-reeve \
  --project=$PROJECT_ID \
  --location=us \
  --uniform-bucket-level-access

gcloud storage buckets add-iam-policy-binding gs://mycompany-reeve \
  --member="serviceAccount:reeve-prod@$PROJECT_ID.iam.gserviceaccount.com" \
  --role=roles/storage.objectAdmin
```

## Adjust configs

Search and replace:

- `mycompany-prod` → your project ID
- `111` → your project number
- `myorg/myrepo` → your repo
- `mycompany-reeve` → your bucket
