"""Generates kis-flows.drawio: one draw.io page per kis flow.

    python3 docs/diagrams/generate.py docs/diagrams/kis-flows.drawio

Edit the pages here rather than in draw.io, so the diagram stays reviewable
and regenerable. Exits 1 if two boxes on a page overlap.
"""
import sys
from xml.sax.saxutils import quoteattr

S = {
    "proc": "rounded=1;whiteSpace=wrap;html=1;fillColor=#dae8fc;strokeColor=#6c8ebf;fontSize=12;",
    "start": "rounded=1;arcSize=50;whiteSpace=wrap;html=1;fillColor=#e1d5e7;strokeColor=#9673a6;fontStyle=1;fontSize=12;",
    "dec": "rhombus;whiteSpace=wrap;html=1;fillColor=#fff2cc;strokeColor=#d6b656;fontSize=11;",
    "ok": "rounded=1;whiteSpace=wrap;html=1;fillColor=#d5e8d4;strokeColor=#82b366;fontSize=12;",
    "bad": "rounded=1;whiteSpace=wrap;html=1;fillColor=#f8cecc;strokeColor=#b85450;fontSize=12;",
    "warn": "rounded=1;whiteSpace=wrap;html=1;fillColor=#ffe6cc;strokeColor=#d79b00;fontSize=12;",
    "file": "shape=note;size=14;whiteSpace=wrap;html=1;fillColor=#f5f5f5;strokeColor=#666666;fontSize=11;align=left;spacingLeft=8;",
    "lane": "rounded=0;whiteSpace=wrap;html=1;fillColor=#fafafa;strokeColor=#bbbbbb;verticalAlign=top;fontStyle=1;fontSize=14;dashed=1;",
    "title": "text;html=1;fontSize=20;fontStyle=1;align=left;verticalAlign=middle;",
    "h2": "text;html=1;fontSize=15;fontStyle=1;align=left;verticalAlign=middle;",
    "note": "rounded=1;whiteSpace=wrap;html=1;fillColor=#ffffff;strokeColor=#999999;dashed=1;fontSize=11;align=left;verticalAlign=top;spacing=8;",
}
EDGE = "edgeStyle=orthogonalEdgeStyle;rounded=1;html=1;endArrow=block;endFill=1;fontSize=11;"
DASH = EDGE + "dashed=1;strokeColor=#888888;"

pages = []


class Page:
    def __init__(self, name):
        self.name, self.cells, self.boxes, self.ids = name, [], [], set()
        pages.append(self)

    def n(self, id, x, y, w, h, label, kind="proc"):
        assert id not in self.ids, id
        self.ids.add(id)
        self.cells.append(
            f'<mxCell id={quoteattr(id)} value={quoteattr(label)} style={quoteattr(S[kind])} vertex="1" parent="1">'
            f'<mxGeometry x="{x}" y="{y}" width="{w}" height="{h}" as="geometry"/></mxCell>')
        if kind not in ("lane", "title", "h2"):
            self.boxes.append((id, x, y, w, h))

    def e(self, src, tgt, label="", dashed=False):
        for i in (src, tgt):
            assert i in self.ids, f"{self.name}: unknown node {i}"
        id = f"e{len(self.cells)}"
        self.cells.append(
            f'<mxCell id="{id}" value={quoteattr(label)} style={quoteattr(DASH if dashed else EDGE)} edge="1" parent="1" source={quoteattr(src)} target={quoteattr(tgt)}>'
            f'<mxGeometry relative="1" as="geometry"/></mxCell>')

    def chain(self, *ids):
        for a, b in zip(ids, ids[1:]):
            self.e(a, b)

    def overlaps(self):
        out = []
        for i, a in enumerate(self.boxes):
            for b in self.boxes[i + 1:]:
                if a[1] < b[1] + b[3] and b[1] < a[1] + a[3] and a[2] < b[2] + b[4] and b[2] < a[2] + a[4]:
                    out.append((a[0], b[0]))
        return out


B = "<br>"

# 1 ── Architecture ──────────────────────────────────────────────
p = Page("1 · Architecture")
p.n("t", 20, 0, 900, 40, "kis: where everything lives", "title")
p.n("L1", 20, 50, 340, 560, "Owner's machine", "lane")
p.n("L2", 420, 50, 340, 560, "Remote (GitHub, GitLab…)", "lane")
p.n("L3", 820, 50, 340, 560, "Partner's machine", "lane")
p.n("o_wt", 40, 100, 300, 60, "<b>Working tree</b>" + B + ".env in plaintext")
p.n("o_f", 40, 190, 300, 60, "<b>clean filter</b> (git add → encrypts)" + B + "deterministic AES-256-GCM")
p.n("o_c", 40, 280, 300, 60, "<b>.git/kis/key</b>" + B + "cached master key (never committed)", "file")
p.n("o_id", 40, 370, 300, 60, "<b>~/.config/kis/identity</b>" + B + "owner's private age key", "file")
p.n("o_cfg", 40, 460, 300, 70, "<b>.git/config + hooks/pre-commit</b>" + B + "local wiring (not part of the repo)", "file")
p.n("r_env", 440, 100, 300, 60, "<b>.env</b> → ciphertext only" + B + "header \\0KIS\\0v1\\0…", "ok")
p.n("r_k", 440, 190, 300, 80, "<b>.kis/keys/owner.age · ana.age</b>" + B + "master key wrapped with age" + B + "to each person's public key", "file")
p.n("r_r", 440, 300, 300, 60, "<b>.kis/recipients.toml</b>" + B + "name → public keys", "file")
p.n("r_g", 440, 390, 300, 60, "<b>.gitencrypt → .gitattributes</b>" + B + "which files to encrypt (managed block)", "file")
p.n("r_a", 440, 480, 300, 60, "<b>AGENTS.md / CLAUDE.md</b>" + B + "kis section for coding agents", "file")
p.n("p_id", 840, 100, 300, 60, "<b>~/.config/kis/identity</b>" + B + "ana's private key", "file")
p.n("p_u", 840, 190, 300, 60, "<b>kis unlock</b>" + B + "opens ana.age with her private key")
p.n("p_c", 840, 280, 300, 60, "<b>.git/kis/key</b>" + B + "cached master key", "file")
p.n("p_s", 840, 370, 300, 60, "<b>smudge filter</b>" + B + "(checkout → decrypts)")
p.n("p_wt", 840, 460, 300, 60, "<b>Working tree</b>" + B + ".env in plaintext", "ok")
p.e("o_wt", "o_f", "git add")
p.e("o_c", "o_f", "uses", True)
p.e("o_f", "r_env", "git push")
p.e("r_k", "p_u", "git pull")
p.e("p_id", "p_u")
p.e("p_u", "p_c")
p.e("p_c", "p_s", "uses", True)
p.e("r_env", "p_s", "git pull")
p.e("p_s", "p_wt")
p.n("leg", 20, 630, 1140, 70,
    "<b>Core idea:</b> only <b>public</b> keys travel (age1… / ssh-ed25519 / github:USER). "
    "Private keys and the plaintext master key never leave each machine. "
    "The remote holds only ciphertext plus the master key wrapped per person. "
    "Without <code>kis unlock</code>, a clone only sees ciphertext.", "note")

# 2 ── kis init ──────────────────────────────────────────────────
p = Page("2 · kis init")
p.n("t", 20, 0, 900, 40, "kis init (owner, once per repository)", "title")
p.n("s", 320, 60, 320, 50, "kis init [-y] [--name] [--identity PATH] [--preset] [--no-agents]", "start")
p.n("d_git", 380, 140, 200, 90, "git ≥ 2.28?", "dec")
p.n("x_git", 720, 155, 300, 60, "Error: found / needed version, why," + B + "\"upgrade git\" (no command)", "bad")
p.n("d_kis", 380, 260, 200, 90, ".kis/ already" + B + "exists?", "dec")
p.n("x_kis", 720, 265, 300, 80, "Refuse, nothing changes." + B + "Set up → kis unlock / kis add" + B + "config.toml lost → restore it (page 10)", "bad")
p.n("p_id", 320, 380, 320, 60, "Pick identity: --identity PATH /" + B + "menu (TTY) / reuse or create age key")
p.n("f_id", 720, 380, 300, 60, "~/.config/kis/identity (0600)" + B + "or ~/.ssh/id_ed25519", "file")
p.n("p_ge", 320, 470, 320, 60, "Write default .gitencrypt:" + B + ".env, .env.* (except .example/.sample/.template)")
p.n("d_st", 380, 560, 200, 90, "Stack detected?" + B + "(*.tf, pom.xml, …)", "dec")
p.n("x_st", 720, 570, 300, 70, "TTY: ask and add the patterns" + B + "No TTY: print kis sync --preset X", "warn")
p.n("p_m", 320, 680, 320, 60, "Generate the <b>master key</b>" + B + "(32 random bytes)")
p.n("p_w", 320, 770, 320, 70, "Cache in .git/kis/key · wrap into" + B + ".kis/keys/YOU.age · recipients.toml · config.toml")
p.n("f_w", 720, 770, 300, 70, "Committed: .kis/*" + B + "Local only: .git/kis/key", "file")
p.n("p_wi", 320, 870, 320, 60, "<b>wireGit</b>: clean/smudge/diff/merge drivers" + B + "in .git/config + pre-commit hook")
p.n("p_at", 320, 960, 320, 60, "SyncAttributes:" + B + ".gitencrypt → block in .gitattributes")
p.n("p_ad", 320, 1050, 320, 60, "git add: .env goes through clean →" + B + "staged <b>encrypted</b>")
p.n("p_ag", 320, 1140, 320, 60, "Add the kis section to AGENTS.md" + B + "(+ CLAUDE.md, …) unless --no-agents")
p.n("d_ig", 380, 1230, 200, 90, "Is .env in" + B + ".gitignore?", "dec")
p.n("x_ig", 720, 1245, 300, 60, "Offer to un-ignore it (!/.env)" + B + "and stage it encrypted")
p.n("d_hi", 380, 1350, 200, 90, "Already committed" + B + "in plaintext?", "dec")
p.n("x_hi", 720, 1365, 300, 60, "Warning: old history holds the" + B + "secret → rotate those credentials", "warn")
p.n("end", 320, 1470, 320, 60, "Print next steps." + B + "<b>Never commits or pushes</b>: the user does", "ok")
p.e("s", "d_git")
p.e("d_git", "x_git", "no")
p.e("d_git", "d_kis", "yes")
p.e("d_kis", "x_kis", "yes")
p.e("d_kis", "p_id", "no")
p.e("p_id", "f_id", "", True)
p.chain("p_id", "p_ge", "d_st")
p.e("d_st", "x_st", "yes")
p.e("d_st", "p_m", "no")
p.e("x_st", "p_m", "", True)
p.chain("p_m", "p_w", "p_wi", "p_at", "p_ad", "p_ag", "d_ig")
p.e("p_w", "f_w", "", True)
p.e("d_ig", "x_ig", "yes")
p.e("d_ig", "d_hi", "no")
p.e("x_ig", "d_hi", "", True)
p.e("d_hi", "x_hi", "yes")
p.e("d_hi", "end", "no")
p.e("x_hi", "end", "", True)

# 3 ── Onboarding ────────────────────────────────────────────────
p = Page("3 · Onboarding (keygen → add → unlock)")
p.n("t", 20, 0, 900, 40, "Adding a partner: only the public key travels", "title")
p.n("L1", 20, 50, 360, 1100, "Partner (ana)", "lane")
p.n("L2", 420, 50, 360, 1100, "Owner / member with access", "lane")
p.n("L3", 820, 50, 320, 1100, "Remote repository", "lane")
p.n("p1", 40, 100, 320, 50, "kis keygen", "start")
p.n("p2", 40, 180, 320, 70, "Creates ~/.config/kis/identity (0600)" + B + "Prints <b>only the public key</b> age1…")
p.n("o1", 440, 300, 320, 60, "Receives age1… over any channel" + B + "(it is public: Slack, email, a PR…)")
p.n("o2", 440, 390, 320, 70, "kis add ana KEY" + B + "KEY = age1… | \"ssh-ed25519 …\" |" + B + "github:ana | ana.pub", "start")
p.n("o3", 440, 490, 320, 70, "ResolveRecipients validates it;" + B + "rejects private, ECDSA and sk- keys")
p.n("o4", 440, 590, 320, 80, "age wraps the master key for ana" + B + "→ .kis/keys/ana.age · recipients.toml" + B + "git add .kis/")
p.n("o5", 440, 700, 320, 50, "git commit && git push")
p.n("o6", 440, 790, 320, 60, "Requirement: the owner must be" + B + "<b>unlocked</b> (needs the master key)", "note")
p.n("r1", 840, 700, 280, 60, ".kis/keys/ana.age" + B + "on the remote", "file")
p.n("p3", 40, 780, 320, 50, "git clone / git pull")
p.n("p4", 40, 860, 320, 50, "kis unlock", "start")
p.n("p5", 40, 940, 320, 70, "Opens ana.age with her private key" + B + "→ gets the master key")
p.n("p6", 40, 1040, 320, 80, "Caches .git/kis/key · wires git ·" + B + "re-checkout → .env in plaintext ✓", "ok")
p.e("p1", "p2")
p.e("p2", "o1", "sends the public key")
p.chain("o1", "o2", "o3", "o4", "o5")
p.e("o5", "r1", "push")
p.e("r1", "p3", "pull")
p.chain("p3", "p4", "p5", "p6")

# 4 ── kis unlock ────────────────────────────────────────────────
p = Page("4 · kis unlock: which key it uses")
p.n("t", 20, 0, 900, 40, "kis unlock: how it picks the private key", "title")
p.n("s", 300, 50, 300, 50, "kis unlock [--identity PATH]", "start")
p.n("d1", 350, 130, 200, 90, "--identity PATH?", "dec")
p.n("a1", 660, 145, 300, 60, "Uses <b>only</b> that file")
p.n("d2", 350, 250, 200, 90, "KIS_PRIVATE_KEY" + B + "set?", "dec")
p.n("d2b", 710, 250, 200, 90, "age, or SSH" + B + "without passphrase?", "dec")
p.n("x2", 1000, 265, 240, 60, "Error: invalid key", "bad")
p.n("a2", 660, 370, 300, 60, "Uses <b>only</b> that key" + B + "(key files are ignored)")
p.n("c", 300, 380, 300, 90, "Candidates, in order:" + B + "1. $KIS_IDENTITY  2. ~/.config/kis/identity" + B + "3. ~/.ssh/id_ed25519  4. ~/.ssh/id_rsa" + B + "(SSH passphrase: asked only if needed)")
p.n("d3", 350, 500, 200, 90, "Found any?", "dec")
p.n("x3", 660, 515, 300, 60, "Error: no key found" + B + "→ kis keygen", "bad")
p.n("u", 300, 620, 300, 70, "<b>UnwrapKeys</b>: for each group, try every" + B + "member's .age file with your keys")
p.n("d4", 350, 720, 200, 90, "Does one open?", "dec")
p.n("x4", 660, 725, 320, 80, "Prints your public keys, exit 1" + B + "<b>⚠ git is NOT wired</b>" + B + "(no filter, no hook)", "bad")
p.n("sv", 300, 850, 300, 60, "ForgetKeys + save .git/kis/key" + B + "and .git/kis/groups/*")
p.n("wi", 300, 940, 300, 50, "wireGit: filters + pre-commit")
p.n("rf", 300, 1020, 300, 50, "Refresh: re-checkout → plaintext")
p.n("rn", 300, 1100, 300, 50, "Re-encrypt anything staged in plaintext")
p.n("end", 300, 1180, 300, 50, "✓ unlocked as NAME", "ok")
p.n("nt", 660, 940, 320, 90, "The private key <b>only</b> opens the master key." + B + "The master key encrypts and decrypts" + B + "the files, so the private key is only" + B + "needed at unlock.", "note")
p.e("s", "d1")
p.e("d1", "a1", "yes")
p.e("d1", "d2", "no")
p.e("d2", "d2b", "yes")
p.e("d2b", "x2", "no")
p.e("d2b", "a2", "yes")
p.e("d2", "c", "no")
p.e("c", "d3")
p.e("d3", "x3", "no")
p.e("d3", "u", "yes")
p.e("a1", "u", "", True)
p.e("a2", "u", "", True)
p.e("u", "d4")
p.e("d4", "x4", "no")
p.e("d4", "sv", "yes")
p.chain("sv", "wi", "rf", "rn", "end")

# 5 ── Everyday git ──────────────────────────────────────────────
p = Page("5 · Everyday git (filters, hook, merge)")
p.n("t", 20, 0, 900, 40, "What kis does inside git", "title")
p.n("hA", 20, 40, 400, 40, "A · git add → clean filter", "h2")
p.n("a1", 120, 90, 260, 50, "git add .env", "start")
p.n("a2", 120, 170, 260, 50, "kis clean filter")
p.n("a3", 150, 250, 200, 90, "Already encrypted?", "dec")
p.n("a3y", 400, 265, 200, 60, "Passed through" + B + "(idempotent)")
p.n("a4", 150, 370, 200, 90, "Group key in" + B + ".git/kis?", "dec")
p.n("a4n", 400, 380, 220, 70, "ERROR: refusing to store in" + B + "plaintext (required=true → git fails)", "bad")
p.n("a5", 120, 490, 260, 60, "Deterministic AES-256-GCM →" + B + "<b>encrypted</b> blob in the index", "ok")
p.e("a1", "a2")
p.e("a2", "a3")
p.e("a3", "a3y", "yes")
p.e("a3", "a4", "no")
p.e("a4", "a4n", "no")
p.e("a4", "a5", "yes")
p.n("hB", 680, 40, 400, 40, "B · checkout / pull → smudge filter", "h2")
p.n("b1", 780, 90, 260, 50, "git checkout / git pull", "start")
p.n("b2", 780, 170, 260, 50, "kis smudge filter")
p.n("b3", 810, 250, 200, 90, "Has a KIS header?", "dec")
p.n("b3n", 1080, 265, 200, 60, "Passed through")
p.n("b4", 810, 370, 200, 90, "Group key here?", "dec")
p.n("b4n", 1080, 380, 200, 70, "Leaves the ciphertext" + B + "(checkout never fails)", "warn")
p.n("b5", 810, 490, 200, 90, "Decrypts?", "dec")
p.n("b5n", 1080, 500, 200, 70, "stderr: cannot decrypt" + B + "leaves the ciphertext", "warn")
p.n("b6", 780, 610, 260, 50, "Plaintext in the working tree", "ok")
p.e("b1", "b2")
p.e("b2", "b3")
p.e("b3", "b3n", "no")
p.e("b3", "b4", "yes")
p.e("b4", "b4n", "no")
p.e("b4", "b5", "yes")
p.e("b5", "b5n", "no")
p.e("b5", "b6", "yes")
p.n("hC", 20, 720, 400, 40, "C · git commit → pre-commit hook", "h2")
p.n("c1", 120, 770, 260, 50, "git commit", "start")
p.n("c2", 120, 850, 260, 50, ".git/hooks/pre-commit → kis precommit")
p.n("c3", 150, 930, 200, 90, ".gitencrypt valid?", "dec")
p.n("c4", 150, 1050, 200, 90, ".gitattributes" + B + "in sync?", "dec")
p.n("c5", 150, 1170, 200, 90, "Right group?", "dec")
p.n("c6", 150, 1290, 200, 90, "Everything encrypted" + B + "in the index?", "dec")
p.n("c7", 120, 1410, 260, 50, "Commit OK", "ok")
p.n("cx", 430, 1100, 200, 120, "<b>Commit blocked</b>" + B + "with the fix:" + B + "kis sync / kis unlock", "bad")
p.e("c1", "c2")
p.e("c2", "c3")
p.e("c3", "c4", "yes")
p.e("c4", "c5", "yes")
p.e("c5", "c6", "yes")
p.e("c6", "c7", "yes")
for d in ("c3", "c4", "c5", "c6"):
    p.e(d, "cx", "no")
p.n("hD", 680, 720, 400, 40, "D · git merge → merge=kis driver", "h2")
p.n("m1", 780, 770, 260, 50, "git merge with changes to .env", "start")
p.n("m2", 780, 850, 260, 60, "driver: kis merge %O %A %B %L %P")
p.n("m3", 780, 940, 260, 70, "Decrypts base / ours / theirs into" + B + ".git/kis/tmp-* (0700)")
p.n("m4", 780, 1040, 260, 60, "git merge-file on the plaintext")
p.n("m5", 780, 1130, 260, 60, "Writes the result <b>encrypted</b> to %A")
p.n("m6", 780, 1220, 260, 80, "smudge → conflicts show in plaintext;" + B + "the object store only sees" + B + "ciphertext", "ok")
p.n("m7", 1080, 850, 220, 70, "Locked clone: the driver" + B + "refuses → normal conflict", "note")
p.chain("m1", "m2", "m3", "m4", "m5", "m6")
p.e("m2", "m7", "no key", True)

# 6 ── CI and cloud agents ───────────────────────────────────────
p = Page("6 · CI and cloud agents")
p.n("t", 20, 0, 900, 40, "CI and cloud agents: KIS_PRIVATE_KEY", "title")
p.n("L1", 20, 50, 420, 620, "Owner", "lane")
p.n("L2", 480, 50, 740, 620, "CI runner / cloud agent (fresh clone)", "lane")
p.n("o1", 40, 100, 380, 50, "kis add ci --new-key [--group prod]", "start")
p.n("o2", 40, 180, 380, 60, "Generates an age key pair <b>in memory</b>" + B + "(the private key never touches disk)")
p.n("o3", 40, 270, 380, 80, "Prints the private key <b>once</b>" + B + "⚠ if an agent runs this, the key ends up" + B + "in its transcript", "warn")
p.n("o4", 40, 380, 380, 60, "Stored as the KIS_PRIVATE_KEY secret" + B + "on the platform (GitHub, Codex, …)")
p.n("o5", 40, 470, 380, 50, "git commit && git push (.kis/keys/ci.age)")
p.n("o6", 40, 560, 380, 80, "<b>\"ci\" is just a name</b> (could be deploy," + B + "codex-bot…). --new-key is what changes" + B + "the behaviour.", "note")
p.n("c1", 620, 100, 300, 50, "Fresh clone (no keys)", "start")
p.n("c2", 670, 180, 200, 90, "Needs the real" + B + "values?", "dec")
p.n("c2n", 960, 195, 240, 70, "build / test /" + B + "kis status --check (no key)", "ok")
p.n("c3", 670, 310, 200, 90, "KIS_PRIVATE_KEY" + B + "set?", "dec")
p.n("c3n", 960, 300, 240, 110, "unlock fails → git has <b>no</b> filter" + B + "and no hook" + B + "⚠ committing .env stores it" + B + "in <b>plaintext</b>", "bad")
p.n("c3m", 960, 440, 240, 70, "Only kis status --check" + B + "in CI catches it", "warn")
p.n("c4", 620, 440, 300, 60, "kis unlock → \"unlocked as ci" + B + "(key from KIS_PRIVATE_KEY)\"")
p.n("c5", 620, 530, 300, 60, "deploy / integration tests" + B + "(.env in plaintext)", "ok")
p.chain("o1", "o2", "o3", "o4", "o5")
p.e("o4", "c3", "platform secret", True)
p.e("c1", "c2")
p.e("c2", "c2n", "no")
p.e("c2", "c3", "yes")
p.e("c3", "c3n", "no")
p.e("c3n", "c3m")
p.e("c3", "c4", "yes")
p.e("c4", "c5")

# 7 ── kis status ────────────────────────────────────────────────
p = Page("7 · kis status: is it safe to push?")
p.n("t", 20, 0, 900, 40, "kis status: what \"safe to push\" means", "title")
p.n("s", 300, 50, 300, 60, "kis status [--check] [--json] [--since REV]" + B + "(needs no key)", "start")
p.n("k1", 300, 130, 300, 50, "1 · .gitencrypt parses without errors")
p.n("k2", 300, 200, 300, 50, "2 · .gitattributes in sync with .gitencrypt")
p.n("k3", 300, 270, 300, 50, "3 · every tracked secret encrypted in the <b>index</b>")
p.n("k4", 300, 340, 300, 50, "4 · encrypted for the right <b>group</b>")
p.n("k5", 300, 410, 300, 50, "5 · encrypted in <b>HEAD</b>")
p.n("k6", 300, 480, 300, 60, "6 · with --since REV: every commit in" + B + "REV..HEAD that adds or changes a secret")
p.n("d", 350, 570, 200, 90, "Any error?", "dec")
p.n("bad", 660, 580, 300, 70, "safe_to_push = false" + B + "--check → exit 1 · each problem with its fix", "bad")
p.n("ok", 300, 700, 300, 60, "safe_to_push = true · exit 0", "ok")
p.n("w", 660, 130, 320, 110, "<b>Warnings</b> (don't block):" + B + "• filters / hook missing (only when unlocked)" + B + "• git &lt; 2.28 (\"upgrade git\", no command)", "warn")
p.n("nc", 660, 270, 320, 150, "<b>Does NOT cover:</b>" + B + "• secrets in other files" + B + "• history before REV" + B + "• ciphertext made with a retired key (issue #8)" + B + "• keyless clone: no warning that the next" + B + "&nbsp;&nbsp;commit would be plaintext", "note")
p.n("out", 20, 130, 240, 200, "<b>Outputs:</b>" + B + "• text: for people" + B + "• --json: for agents" + B + "&nbsp;&nbsp;(safe_to_push, files[].state," + B + "&nbsp;&nbsp;problems[].severity / fix)" + B + "• --check: the exit code" + B + "• --since origin/main: the whole PR", "note")
p.chain("s", "k1", "k2", "k3", "k4", "k5", "k6", "d")
p.e("d", "bad", "yes")
p.e("d", "ok", "no")

# 8 ── remove / rotate + issue #8 ────────────────────────────────
p = Page("8 · kis remove / rotate (+ issue #8)")
p.n("t", 20, 0, 1200, 40, "Revoking and rotating, and what happens to the other partners", "title")
p.n("s", 40, 50, 300, 60, "kis remove bob" + B + "[--group G] [--no-rotate] [--force]", "start")
p.n("d1", 90, 130, 200, 90, "Last member?", "dec")
p.n("x1", 380, 145, 220, 60, "Error: nobody could decrypt", "bad")
p.n("d2", 90, 250, 200, 90, "Do you hold the" + B + "group key?", "dec")
p.n("x2", 380, 265, 220, 60, "Error (or --no-rotate)", "bad")
p.n("d3", 90, 370, 200, 90, "Uncommitted" + B + "changes?", "dec")
p.n("x3", 380, 385, 220, 60, "Error (or --force)", "bad")
p.n("r1", 40, 490, 300, 50, "Delete .kis/keys/(G/)bob.age")
p.n("r2", 40, 570, 300, 80, "<b>rotateGroup</b>: NEW master key →" + B + "wrapped for the remaining members →" + B + "git add --renormalize")
p.n("r3", 40, 680, 300, 60, "If in no group anymore:" + B + "removed from recipients.toml")
p.n("r4", 40, 770, 300, 60, "Warning: bob can read the history →" + B + "rotate the real credentials", "warn")
p.n("r5", 40, 860, 300, 50, "commit && push")
p.n("r6", 40, 940, 300, 50, "kis rotate = the same rotateGroup" + B + "without removing anyone", "note")
p.n("hq", 700, 50, 500, 40, "The other partners (issue #8)", "h2")
p.n("q1", 700, 110, 300, 50, "ana: git pull", "start")
p.n("q2", 700, 190, 300, 70, "smudge with the <b>old</b> cached key →" + B + "\"cannot decrypt\", .env stays ciphertext", "warn")
p.n("q3", 750, 290, 200, 90, "Does ana run" + B + "kis unlock?", "dec")
p.n("q3y", 1040, 305, 240, 60, "Gets the new key ✓", "ok")
p.n("q4", 700, 420, 300, 70, "ana commits .env →" + B + "clean encrypts with the <b>OLD</b> key", "bad")
p.n("q5", 700, 520, 300, 90, "bob (removed) reads it in plaintext ·" + B + "the owner can't decrypt it ·" + B + "kis status says safe ✗", "bad")
p.n("q6", 700, 640, 300, 80, "The CLI says \"Partners who pull will" + B + "get the new key automatically\"" + B + "→ false", "note")
p.e("s", "d1")
p.e("d1", "x1", "yes")
p.e("d1", "d2", "no")
p.e("d2", "x2", "no")
p.e("d2", "d3", "yes")
p.e("d3", "x3", "yes")
p.e("d3", "r1", "no")
p.chain("r1", "r2", "r3", "r4", "r5")
p.e("r5", "q1", "push", True)
p.e("q1", "q2")
p.e("q2", "q3")
p.e("q3", "q3y", "yes")
p.e("q3", "q4", "no")
p.e("q4", "q5")

# 9 ── lock / sync ───────────────────────────────────────────────
p = Page("9 · kis lock / kis sync")
p.n("t", 20, 0, 900, 40, "kis lock and kis sync", "title")
p.n("l1", 40, 60, 300, 50, "kis lock [--force]", "start")
p.n("l2", 90, 140, 200, 90, "Uncommitted changes" + B + "to secrets?", "dec")
p.n("l2y", 380, 155, 220, 60, "Error (or --force:" + B + "the changes are lost)", "bad")
p.n("l3", 40, 270, 300, 60, "Delete .git/kis/key and" + B + ".git/kis/groups/*")
p.n("l4", 40, 360, 300, 60, "Re-checkout → working tree" + B + "in ciphertext")
p.n("l5", 40, 450, 300, 70, "git stays wired: git add of" + B + "plaintext now <b>fails</b> (fail closed)", "ok")
p.e("l1", "l2")
p.e("l2", "l2y", "yes")
p.e("l2", "l3", "no")
p.chain("l3", "l4", "l5")
p.n("s1", 680, 60, 340, 60, "You edit .gitencrypt" + B + "(e.g. config/secrets.json or .env.prod @prod)", "start")
p.n("s2", 680, 150, 340, 50, "kis sync [--preset LIST]")
p.n("s3", 680, 230, 340, 50, "Apply presets (if asked for)")
p.n("s4", 680, 300, 340, 60, "Create new @groups" + B + "(you as the first member)")
p.n("s5", 680, 380, 340, 50, "Regenerate the .gitattributes block")
p.n("s6", 680, 450, 340, 60, "Re-encrypt files that joined ·" + B + "decrypt files that left (needs the key)")
p.n("s7", 680, 530, 340, 50, "Refresh the kis section of AGENTS.md")
p.n("s8", 680, 600, 340, 60, "Offer to stage untracked secrets", "ok")
p.n("s9", 680, 690, 340, 60, "Without sync: pre-commit and kis status" + B + "block (.gitattributes out of date)", "note")
p.chain("s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8")

# 10 ── init with existing state ─────────────────────────────────
p = Page("10 · kis init when things already exist")
p.n("t", 20, 0, 1100, 40, "kis init when keys or kis files already exist", "title")
p.n("s", 300, 60, 300, 50, "kis init", "start")
p.n("d1", 350, 140, 200, 90, ".kis/config.toml" + B + "exists?", "dec")
p.n("r1", 660, 150, 360, 70, "<b>Refuse</b>: \"already set up\" → kis unlock / kis add." + B + "Wrapped keys and other partners untouched", "bad")
p.n("d2", 350, 260, 200, 90, ".kis/ exists" + B + "anyway?", "dec")
p.n("r2", 660, 255, 360, 100, "<b>Broken repo: refuse</b>. config.toml is missing;" + B + "restore it from git history. Every kis command" + B + "refuses the same way (status --json: error)." + B + "A new init would drop the partners", "bad")
p.n("d3", 350, 380, 200, 90, "Personal key found?" + B + "(~/.config/kis/identity," + B + "or --identity PATH)", "dec")
p.n("d4", 350, 500, 200, 90, "Valid key?", "dec")
p.n("r4", 660, 500, 360, 90, "<b>Refuse with a clear message</b>:" + B + "\"found a key file at PATH, but it can't be opened\"" + B + "or \"PATH is a public key\". Nothing is written;" + B + "the key file is left exactly as it was", "bad")
p.n("r5", 300, 620, 300, 70, "<b>Reuse it</b>: same public key," + B + "file never rewritten", "ok")
p.n("r6", 20, 395, 240, 60, "Create a new age key" + B + "(O_EXCL, 0600)")
p.n("m", 300, 730, 300, 80, "<b>New master key for this repo</b>," + B + "wrapped to your public key →" + B + ".kis/keys/YOU.age + recipients.toml")
p.n("n1", 660, 620, 360, 190,
    "<b>Facts behind these branches:</b>" + B +
    "• One personal key per machine; one master key" + B + "&nbsp;&nbsp;per repository." + B +
    "• Public keys live in .kis/recipients.toml." + B +
    "• Wrapped master keys live in .kis/keys/NAME.age." + B +
    "• The private key never enters the repo." + B + B +
    "Tested in cmd/kis/testdata/script/" + B + "init_existing_state.txtar (scenarios 1–5)", "note")
p.e("s", "d1")
p.e("d1", "r1", "yes · scenario 3")
p.e("d1", "d2", "no")
p.e("d2", "r2", "yes · scenario 4")
p.e("d2", "d3", "no")
p.e("d3", "r6", "no")
p.e("d3", "d4", "yes")
p.e("d4", "r4", "no · scenarios 2, 5")
p.e("d4", "r5", "yes · scenario 1")
p.e("r5", "m")
p.e("r6", "m")

# ── write ───────────────────────────────────────────────────────
bad = False
for pg in pages:
    for a, b in pg.overlaps():
        print(f"OVERLAP {pg.name}: {a} / {b}")
        bad = True
out = ['<mxfile host="app.diagrams.net" agent="kis-docs">']
for i, pg in enumerate(pages):
    out.append(f'<diagram id="p{i+1}" name={quoteattr(pg.name)}><mxGraphModel dx="1400" dy="900" grid="1" gridSize="10" guides="1" tooltips="1" connect="1" arrows="1" fold="1" page="1" pageScale="1" pageWidth="1300" pageHeight="1600" math="0" shadow="0"><root><mxCell id="0"/><mxCell id="1" parent="0"/>')
    out.extend(pg.cells)
    out.append("</root></mxGraphModel></diagram>")
out.append("</mxfile>")
with open(sys.argv[1], "w") as f:
    f.write("\n".join(out) + "\n")
print(f"{len(pages)} pages, {sum(len(p.cells) for p in pages)} cells")
sys.exit(1 if bad else 0)
