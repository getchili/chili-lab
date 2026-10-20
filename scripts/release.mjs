// Publie une release GitHub : crée la release vX.Y.Z (si elle n'existe pas)
// et y envoie les fichiers listés dans package.json → "release".
//
//   npm run release              construit puis publie
//   npm run release -- --draft   en brouillon (à valider sur GitHub)
//   node scripts/release.mjs     publie sans reconstruire
//
// Jeton GitHub : variable d'environnement GITHUB_TOKEN (ou GH_TOKEN), droit
// « Contents : Read and write » sur le dépôt. Il n'est jamais écrit sur disque.

import { readFileSync, existsSync } from "node:fs";
import { basename, join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
const cfg = pkg.release;
const draft = process.argv.includes("--draft");

const fail = (msg) => { console.error(`\n✗ ${msg}`); process.exit(1); };

const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
if (!token) {
  fail(`Jeton GitHub manquant. Crée-le sur https://github.com/settings/personal-access-tokens/new
  (dépôt ${cfg.repo}, permission « Contents : Read and write »), puis dans PowerShell :
    $env:GITHUB_TOKEN = "ton_jeton"`);
}

// Version lue dans le code source (ex. const version = "0.1.0").
const versionSrc = readFileSync(join(root, cfg.versionFile), "utf8");
const m = versionSrc.match(/const version = "([^"]+)"/);
if (!m) fail(`version introuvable dans ${cfg.versionFile}`);
const version = m[1];
const tag = `v${version}`;

const assets = cfg.assets.map((a) => join(root, a.replaceAll("{version}", version)));
for (const a of assets) if (!existsSync(a)) fail(`fichier manquant : ${a} (lance d'abord le build)`);

const api = async (method, url, body, headers = {}) => {
  const r = await fetch(url.startsWith("http") ? url : `https://api.github.com${url}`, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      Accept: "application/vnd.github+json",
      "X-GitHub-Api-Version": "2022-11-28",
      "User-Agent": "getchili-release",
      ...headers,
    },
    body,
  });
  if (r.status === 404 && method === "GET") return null;
  if (!r.ok) {
    const text = await r.text();
    if (r.status === 401) fail("jeton GitHub refusé (expiré ou invalide)");
    if (r.status === 403) fail(`accès refusé à ${cfg.repo} : le jeton a-t-il « Contents : Read and write » ?\n${text}`);
    if (r.status === 404) fail(`dépôt ${cfg.repo} introuvable (créé ? jeton autorisé dessus ?)`);
    if (r.status === 422 && text.includes("empty")) fail(`le dépôt ${cfg.repo} est vide : pousse au moins un commit avant la première release`);
    fail(`GitHub ${r.status} sur ${method} ${url}\n${text}`);
  }
  return r.status === 204 ? null : r.json();
};

console.log(`Release ${cfg.repo} ${tag}${draft ? " (brouillon)" : ""}`);

let release = await api("GET", `/repos/${cfg.repo}/releases/tags/${tag}`);
if (release) {
  console.log(`  la release ${tag} existe déjà : mise à jour des fichiers`);
} else {
  release = await api("POST", `/repos/${cfg.repo}/releases`, JSON.stringify({
    tag_name: tag,
    name: `${cfg.name} ${version}`,
    body: cfg.notes.replaceAll("{version}", version),
    draft,
    generate_release_notes: true,
  }));
  console.log(`  release créée`);
}

for (const file of assets) {
  const name = basename(file);
  const old = release.assets.find((a) => a.name === name);
  if (old) await api("DELETE", `/repos/${cfg.repo}/releases/assets/${old.id}`);
  const data = readFileSync(file);
  process.stdout.write(`  envoi de ${name} (${(data.length / 1048576).toFixed(1)} Mo)… `);
  await api("POST",
    `https://uploads.github.com/repos/${cfg.repo}/releases/${release.id}/assets?name=${encodeURIComponent(name)}`,
    data, { "Content-Type": "application/octet-stream" });
  console.log("ok");
}

console.log(`\n✓ ${release.html_url}`);
