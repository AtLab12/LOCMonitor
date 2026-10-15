class Locmonitor < Formula
  desc "Track committed pull request size after each Git commit"
  homepage "https://github.com/AtLab12/LOCMonitor"
  url "https://github.com/AtLab12/LOCMonitor/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "e865a8d725859a122fbf2cf4caaacf0c359db211e28409a75efb3cc63ca80afd"
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

      In a repository, run:
        locmonitor init

      Edit .locmonitor.json to set your PR base, line budget, and exclusions.
    EOS
  end

  test do
    system "git", "init", "-b", "main"
    system "git", "config", "user.email", "test@example.com"
    system "git", "config", "user.name", "Homebrew Test"
    system "git", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "base"
    system "git", "checkout", "-b", "feature"
    (testpath/"example.txt").write("one\ntwo\n")
    system "git", "add", "example.txt"
    system "git", "-c", "commit.gpgsign=false", "commit", "-m", "feature"
    assert_match "2/400 LOC", shell_output("#{bin}/locmonitor status")
    (testpath/"vendor/nested").mkpath
    (testpath/"vendor/nested/library.txt").write("excluded folder\n")
    (testpath/"image.svg").write("excluded type\n")
    system "git", "add", "vendor", "image.svg"
    system "git", "-c", "commit.gpgsign=false", "commit", "-m", "excluded files"
    (testpath/".locmonitor.json").write <<~JSON
      {"exclude_extensions": [".svg"], "exclude_folders": ["vendor"]}
    JSON
    assert_match "2/400 LOC", shell_output("#{bin}/locmonitor status")
  end
end
