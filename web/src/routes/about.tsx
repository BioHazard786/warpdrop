import { Section } from "@/components/section";
import { IconBrandGithub, IconExternalLink } from "@tabler/icons-react";
import { fetchConfig } from "@/transfer/signaling";
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/about")({
  // the web app is embedded in the server, so the server's version is the app's version
  loader: () =>
    fetchConfig().then(
      (c) => c.version,
      () => undefined,
    ),
  component: About,
});

const GITHUB = "https://github.com/BioHazard786";

type App = {
  name: string;
  blurb: string;
  live?: string;
  repo: string;
  here?: boolean;
};

// Keep in sync with the same list in Warpstash's about page and the Android app (ui/screens/About.kt).
const APPS: App[] = [
  {
    name: "Warpstash",
    blurb:
      "Temporary file hosting with expiry, burn-after-reading and optional passwords.",
    live: "https://stash.warplabs.info",
    repo: `${GITHUB}/warpstash`,
  },
  {
    name: "Warpdrop",
    blurb:
      "Privacy-first, self-hostable file sharing from device to device. Like AirDrop, without the cloud.",
    live: "https://drop.warplabs.info",
    repo: `${GITHUB}/warpdrop`,
    here: true,
  },
  {
    name: "Warpvault",
    blurb:
      "Zero-knowledge secret sharing: encrypted in your browser, so the server never sees the plaintext.",
    live: "https://vault.warplabs.info",
    repo: `${GITHUB}/warpvault`,
  },
  {
    name: "Warpkey",
    blurb:
      "Self-hosted credential vault for small teams: logins, API keys and SSH keys by project, with every reveal audited.",
    repo: `${GITHUB}/warpkey`,
  },
  {
    name: "Warptv",
    blurb:
      "Browse and watch 30,000+ free live TV channels in a fast, local-first app with a retro handheld look.",
    live: "https://tv.warplabs.info",
    repo: `${GITHUB}/warptv`,
  },
];

const link =
  "inline-flex items-center gap-1 underline underline-offset-4 hover:text-foreground";

function About() {
  const version = Route.useLoaderData();
  return (
    <>
      <Section title="The Warp family">
        <ul className="flex flex-col gap-3">
          {APPS.map((app) => (
            <li
              key={app.name}
              className="flex flex-col gap-2 rounded-2xl border bg-card p-4 text-card-foreground"
            >
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="font-heading text-sm font-medium">{app.name}</h2>
                {app.here && (
                  <span className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                    you are here
                  </span>
                )}
              </div>
              <p className="text-sm leading-relaxed text-muted-foreground">
                {app.blurb}
              </p>
              <div className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-muted-foreground">
                {app.live && (
                  <a
                    href={app.live}
                    target="_blank"
                    rel="noreferrer"
                    className={link}
                  >
                    {app.live.replace("https://", "")}{" "}
                    <IconExternalLink className="size-3.5" />
                  </a>
                )}
                <a
                  href={app.repo}
                  target="_blank"
                  rel="noreferrer"
                  className={link}
                >
                  <IconBrandGithub className="size-3.5" /> source
                </a>
              </div>
            </li>
          ))}
        </ul>
      </Section>

      <Section title="Version">
        <p className="font-mono text-sm text-muted-foreground">
          WarpDrop {version ?? "unknown"}
        </p>
      </Section>

      <Section title="The developer">
        <p className="text-sm leading-relaxed text-muted-foreground">
          Built and maintained by Mohd Zaid (
          <a href={GITHUB} target="_blank" rel="noreferrer" className={link}>
            @BioHazard786
          </a>
          ). Warp is a family of open-source, self-hostable utilities: use the
          hosted versions, or run your own. Bug reports, ideas and pull requests
          are welcome.
        </p>
      </Section>
    </>
  );
}
