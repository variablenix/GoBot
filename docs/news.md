# News plugin

The news plugin uses NewsAPI and requires an API key.

Enable it in config.yaml:

~~~yaml
plugins:
  news:
    enabled: true
    api_key: ""
    max_results: 3
~~~

Leave api_key empty in Git and set the secret in .env:

~~~env
BOT_NEWS_API_KEY=your-newsapi-key
~~~

Usage:

~~~text
!news
!news linux
~~~

!news shows top US headlines. A query searches recent English-language
articles. If no key is configured, GoBot responds that the news plugin is not
configured.
