# Banter, quotes, and fortune

The optional banter plugin adds occasional personality without constant
channel noise. It replies only when GoBot is directly addressed, ignores
!commands, and may not reply to every message.

Enable it with:

~~~yaml
plugins:
  banter:
    enabled: true
    probability: 0.25
    quotes_file: "quotes/banter.txt"
    fortune_dir: "/path/to/fortune-curated"
~~~

Behavior:

- chooses a quote at random
- loads plain text files from quotes/
- splits long quotes into safe IRC-sized chunks
- optionally reads classic fortune files
- keeps automatic replies bounded by the normal outbound rate limit

The quote command uses the same configured quote sources:

~~~text
!quote
~~~

GoBot reads files directly and never shells out to the fortune command. This
keeps behavior predictable and avoids command-execution surprises.

## Curated fortune files

The recommended setup is to install fortune data packages, then copy only the
collections you want into a separate directory. This keeps the bot's quote
sources explicit instead of loading every system collection.

On Debian or Ubuntu:

~~~sh
sudo apt update && sudo apt install -y fortune-mod fortunes-min fortunes
mkdir -p "$HOME/fortune-curated"
for quote in computers linux science tao wisdom; do
  source_file="$(find /usr/share/games/fortunes /usr/share/fortune -maxdepth 1 -type f -name "$quote" -print -quit 2>/dev/null)"
  if [ -n "$source_file" ]; then
    cp "$source_file" "$HOME/fortune-curated/$quote"
  else
    echo "Warning: fortune collection not found: $quote" >&2
  fi
done
~~~

Package names and source directories vary by distribution. Locate installed
collections with:

~~~sh
find /usr/share/games/fortunes /usr/share/fortune -maxdepth 1 -type f 2>/dev/null | sort
~~~

Add or remove names in the loop to choose a different curated set. Classic
fortune files use % delimiters, which GoBot parses directly. Files ending in
.dat or .u8 are ignored.

Point both plugins at the curated directory:

~~~yaml
plugins:
  banter:
    enabled: true
    probability: 0.25
    quotes_file: "quotes/banter.txt"
    fortune_dir: "/path/to/fortune-curated"
  quote:
    enabled: true
    fortune_dir: "/path/to/fortune-curated"
~~~

Use an expanded absolute path in YAML; environment variables such as $HOME
are not expanded inside configuration values. Every regular file in the
configured directory is loaded. The repository's quotes/banter.txt is loaded
automatically, so OS fortune packages are not needed if you only want the
built-in quotes.
