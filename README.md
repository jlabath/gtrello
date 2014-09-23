### Github trello integration

This is the Appengine part of the github/trello integration used at my workplace.

It is meant to enable team of developers to automatically include commit messages on trello cards.

#### Features:

* Open source (hence not sensitive to me getting hit by a bus)
* HMAC verification of hooks fired by github
* unlimited number of trello cards can be included in each commit message
* move card functionality (each card can be moved to any list on the same board)

#### Usage:

To use this you need Appengine Go [SDK](https://developers.google.com/appengine/downloads#Google_App_Engine_SDK_for_Go).

On how to use Appengine with Go [visit here](https://developers.google.com/appengine/docs/go/gettingstarted/introduction).

To set up the webhooks on Github.

* Set the url to gadvhook.appspot.com
* Content-type: application/json
* Set the secret to _secret_ enter the same _secret_ to config.json.

To configure trello API access.

Basically you want token with no expire set.
[More info here](https://trello.com/docs/gettingstarted/index.html#getting-an-application-key).
Do not forget to enter your Trello API key and token into config.json.

#### Syntax:

When your commit message looks like this


    This is mine fantastic edit.
    https://trello.com/c/2lYMoS5p/592-revenue-streams
    https://trello.com/c/1lKMjS5p/593-workflow-changes move to QA/Review


Two comments will be made in trello one for each card, plus the workflow-changes card will be moved to QA/Review list.

More info about the syntax [here](https://github.com/jlabath/cmsgparser).
