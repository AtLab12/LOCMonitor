class Locmonitor < Formula
  desc "Track committed pull request size after each Git commit"
  homepage "https://github.com/AtLab12/LOCMonitor"
  url "https://github.com/AtLab12/LOCMonitor/archive/refs/tags/v0.3.0.tar.gz"
  sha256 "ff2a9af291f5d342b6d55d44ce10e660a017aebefcd8273236787a855c82da89"
  license "MIT"
  head "https://github.com/AtLab12/LOCMonitor.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}"), "./cmd/locmonitor"
  end

  def caveats
    <<~EOS
      Add to ~/.zshrc:
        eval "$(locmonitor shell-init zsh)"

      Create your global config:
        locmonitor config init

      Edit ~/.config/locmonitor/config.json to list repositories and their settings.
      Or run locmonitor init inside each repository to register its path.
    EOS
  end

  test do
    ENV["LOCMONITOR_CONFIG"] = (testpath/"settings/config.json").to_s
    system "git", "init", "-b", "main"
    system "git", "config", "user.email", "test@example.com"
    system "git", "config", "user.name", "Homebrew Test"
    system "git", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "base"
    system "git", "checkout", "-b", "feature"
    system bin/"locmonitor", "init"
    (testpath/"example.txt").write("one\ntwo\n")
    system "git", "add", "example.txt"
    system "git", "-c", "commit.gpgsign=false", "commit", "-m", "feature"
    assert_match "2/400 LOC", shell_output("#{bin}/locmonitor status")
    (testpath/"vendor/nested").mkpath
    (testpath/"vendor/nested/library.txt").write("excluded folder\n")
    (testpath/"image.svg").write("excluded type\n")
    system "git", "add", "vendor", "image.svg"
    system "git", "-c", "commit.gpgsign=false", "commit", "-m", "excluded files"
    config = JSON.parse((testpath/"settings/config.json").read)
    settings = config.fetch("repositories").values.first
    settings["exclude_extensions"] = [".svg"]
    settings["exclude_folders"] = ["vendor"]
    File.write(testpath/"settings/config.json", JSON.generate(config))
    assert_match "2/400 LOC", shell_output("#{bin}/locmonitor status")
  end
end
