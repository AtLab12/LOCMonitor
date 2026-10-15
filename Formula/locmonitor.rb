class Locmonitor < Formula
  desc "Track committed pull request size after each Git commit"
  homepage "https://github.com/AtLab12/LOCMonitor"
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
    system "git", "add", "."
    system "git", "-c", "commit.gpgsign=false", "commit", "-m", "feature"
    assert_match "2/400 LOC", shell_output("#{bin}/locmonitor status")
  end
end
