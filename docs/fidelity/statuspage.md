# Statuspage export fidelity

What `outpage migrate --from=statuspage --dry-run` maps, and what it honestly
does not. No fake 100%.

## Components

| Statuspage status | Outpage component status | Result |
|---|---|---|
| `operational` | `operational` | mapped |
| `degraded_performance` | `degraded` | mapped |
| `partial_outage` | `partial` | mapped |
| `major_outage` | `major` | mapped |
| `under_maintenance` | `maintenance` | mapped |

Per-component extra fields: `only_show_if_degraded=true` (false is the
harmless default) and any `custom_fields` have no Outpage equivalent yet —
the component imports, the fields drop, and the report marks the component
`partial` with the dropped field named. An unmappable status value is
reported as `status:<original>` in the dropped list.

## Incidents

| Statuspage field | Outpage equivalent | Result |
|---|---|---|
| `name` | incident title | mapped |
| `status` (`investigating`/`identified`/`monitoring`/`resolved`) | same vocabulary | mapped |
| `impact` (`minor`/`major`/`critical`/`none`) | same | mapped |
| `components` | incident component link | mapped |
| `updates[].status`, `updates[].body` | incident updates | mapped |
| `updates[].html_body`, custom HTML bodies | dropped (plain text only) | partial |

## Scheduled maintenances

`name`, `status` (`scheduled`/`in_progress`/`completed`), `starts_at`,
`ends_at`, `components`: mapped. `updates` become the maintenance's update
log.

## Subscribers

| Type | Result |
|---|---|
| `email` | mapped — imported into the local list (synthetic fixtures only in git) |
| `webhook` | mapped — URL imports; **signing secret does not exist in the export**, must be re-entered (risk `webhook.secrets`) |
| `sms` | unmapped — Outpage does not operate an SMS gateway, by scope lock |

## Page metadata

| Statuspage field | Outpage equivalent | Result |
|---|---|---|
| `page.name` | public page title | mapped |
| `page.id`, `page.url`, `page.created_at` | no equivalent (custom domain is your reverse proxy — see docs/custom-domain.md) | partial (fields dropped, named in the report) |

Page theming/HTML, custom CSS/JS: the public page is server-rendered from Outpage's own template; theme fields never migrate.

## Never migrated (unmapped by design)

- `metrics_data` / uptime history: Outpage is a status page, not a monitor —
  key `metrics.uptime`.
- Page theming/HTML, custom CSS/JS: the public page is server-rendered from
  Outpage's own template — key `page.theme`.
- Team/member accounts and Atlassian SSO: no admin accounts exist to map —
  key `team.accounts`.

## Risks always reported

- `webhook.secrets` — re-enter signing secrets after a real import.
- `subscriber.reconfirm` — imported email lists may include addresses that
  never confirmed; re-confirm before first send (anti-spam hygiene).
