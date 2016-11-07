# pipeline

`pipeline` is a tool to help with quick iteration on complex shell pipelines.

Pipe some data into `pipeline`, and edit your pipeline while watching the
output in real time. When you're satisfied, hit `return` to have its output
piped to the next stage. Or, hit `ctrl-c` or `escape` to cancel and exit 1 with
no output.

Evaluation will be 'off' until you terminate the pipeline with a `;`, making
it easier to avoid running bad intermediate commands (like, if the prefix of
a command was `rm...`).

Inspired by, but quite different from, https://github.com/peco/peco.

## Fancy video
[![asciicast](https://asciinema.org/a/cmqnr4ou5s6telprgwrj14ld4.png)](https://asciinema.org/a/cmqnr4ou5s6telprgwrj14ld4)
