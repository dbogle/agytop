# Contributing to agytop ⚡

Thank you for your interest in contributing to `agytop`! This guide will help you get set up for local development.

---

## 🛠️ Development Setup

### Prerequisites

* **Go 1.22+**: [Install Go](https://go.dev/dl/)
* **Git**

### Clone & Build

```bash
# Clone the repository
git clone https://github.com/dbogle/agytop.git
cd agytop

# Install dependencies
go mod download

# Run unit and integration tests
go test -v -race ./...

# Build the binary locally
go build -o bin/agytop ./cmd/agytop

# Run with demo sidecars
./bin/agytop --demo
```

---

## 🧪 Testing

Always ensure that tests pass before submitting a Pull Request:

```bash
# Run all tests with race detector
go test -v -race ./...

# Run static analysis
go vet ./...
```

---

## 📬 Pull Request Guidelines

1. **Create a branch** for your feature or bugfix (`git checkout -b feature/my-new-feature`).
2. **Keep changes focused** and adhere to existing code formatting (`gofmt -s -w .`).
3. **Add unit tests** for new features or bug fixes.
4. **Update documentation** in `README.md` if adding or changing keybindings, CLI flags, or schema definitions.
5. **Open a PR** against the `main` branch with a clear description of the changes.

---

## 📄 License

By contributing to `agytop`, you agree that your contributions will be licensed under the project's [MIT License](LICENSE).
