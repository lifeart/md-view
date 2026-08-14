# Evil Document

Raw HTML and dangerous URLs that must be neutralized by the sanitizer.

<script>alert("xss")</script>

<img src="x" onerror="alert('xss')">

<iframe src="https://example.com"></iframe>

<a href="javascript:alert('xss')">js-scheme raw html link</a>

[js-scheme markdown link](javascript:alert('xss'))

[traversal outside scope](../../../../etc/passwd)

<div onclick="alert('xss')">clickable div</div>

Normal paragraph that must survive sanitization.
