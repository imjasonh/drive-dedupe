[Try it out!](https://drive-dedupe.appspot.com/)
-----

**(This is not owned or developed by Google, Inc.)**

Wait, it's going to read my files?
-----

Nope! This uses the Google Drive API and reads only metadata about your files,
specifically the [MD5](http://en.wikipedia.org/wiki/MD5) hash, filename and
file size of your files. It collects instances where multiple files have the
same name and hash, and adds up how much space you could save by deleting
duplicate files.

It only requests permission to read metadata from you, so it couldn't read,
copy or modify file contents even if it wanted to, or as the result of a bug.

Does it actually delete anything?
-----

Nope.

I might add the ability to delete duplicates (with permission) later, but for
now it just reports on how much of your Drive quota you're wasting with
duplicate files.


----------

License
-----

    Copyright 2015 Jason Hall

    Licensed under the Apache License, Version 2.0 (the "License");
    you may not use this file except in compliance with the License.
    You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

    Unless required by applicable law or agreed to in writing, software
    distributed under the License is distributed on an "AS IS" BASIS,
    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
    See the License for the specific language governing permissions and
    limitations under the License.

