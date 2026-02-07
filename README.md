# GitPulse - AI-powered auto-commit tool

## Usage (other projects / demo)

**One-time setup in any repo:**

```bash
cd /path/to/your/project
gitpulse init
```

This creates `.gitpulse/config.yaml` and adds `.gitpulse/` and `.gitpulse.pid` to `.gitignore`.

**Run from that directory:**

```bash
cd /path/to/your/project
gitpulse
```

**Or run from anywhere (no need to cd):**

```bash
gitpulse -C /path/to/your/project
# or
gitpulse /path/to/your/project
```

**Trigger commit & push** (with daemon running in that project):

```bash
cd /path/to/your/project && gitpulse push
# or from anywhere:
gitpulse push -C /path/to/your/project
```

**Dashboard** (view history):

```bash
gitpulse dashboard -C /path/to/your/project --port 8080
```
