# Copyright 2022 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

gcloud iam service-accounts create gke-prober-sa --display-name "gke-prober"
gcloud iam service-accounts add-iam-policy-binding \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:${PROJECT_ID}.svc.id.goog[gke-prober-system/gke-prober]" \
  gke-prober-sa@${PROJECT_ID}.iam.gserviceaccount.com

# monitoring.metricDescriptors.list needed for prom-to-sd
gcloud iam roles create gkeprober --project ${PROJECT_ID} \
  --permissions=monitoring.metricDescriptors.create,monitoring.metricDescriptors.list,monitoring.timeSeries.create

gcloud projects add-iam-policy-binding \
  --role projects/${PROJECT_ID}/roles/gkeprober \
  --member "serviceAccount:gke-prober-sa@${PROJECT_ID}.iam.gserviceaccount.com" \
  ${PROJECT_ID}

kubectl annotate sa gke-prober \
  -n gke-prober-system \
  iam.gke.io/gcp-service-account=gke-prober-sa@${PROJECT_ID}.iam.gserviceaccount.com