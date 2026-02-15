There are no ears in these walls, so speak freely. Hi and welcome, this app was made with privacy in mind, and there's no such thing as privacy without consent, so be free if you want to either read the code or modify it, that's cool and thanks a lot. Please, any ideas or upgrades you have, send them to me, I'll gladly work on them too.

You should know that the code in this repository isn't enough to run the project. First of all, you need the Tor Expert Bundle, you can get it from the [Tor Project website](https://www.torproject.org/download/tor/), extract it and copy the files into the project folder, they should be laid out like this:

* `tor.exe`
* `tor-expert-bundle`
* `Rizoma.exe`

I'm not sure it will work if you don't more tor.exe to the main folder like that.

Second of all, remember Windows doesn't seem to like Go apps, even the ones you compiled yourself. Windows will tell you that it might be a virus and you should be wary and scared and all that, don't be, you can check the code and compile it yourself if you want to, or don't use the app, it's not necessary for anything.

Also, if you want the icon to work when compiling, you will need winres. Just use the typical "go install github.com/tc-hib/go-winres@latest", and "go-winres make" before the "go build .". It will compile without winres too, and you're not missing much, it's just a poorly taken screenshot of the ASCII Rose.

Just in case you don't want to do any of that, theres RIZOMA+Tor.zip, that one has everything, just extract and enjoy.

GENERAL TIPS

* The password you input the first time you open the app will be automatically asigned as your master password, remember it, but you can change it later.
* You should know that while hosting, multiple people can join on you, you can have your own chat group, though the more people there are, the laggier it gets.
* Save contacts, you can connect to them faster by using /contact `<alias>` `<secret>`.
* You can also quickhost by using /host `<secret>`.
* Never EVER share your rizoma.salt (contains your password security salt), your rizoma_config.json (contains your personal settings and contacts), or your rizoma_key.pem (contains your unique Tor address private key).
* Pre-heat is kinda janky, but can save time when it works.


Thanks for reading,
I will remain at your service
Gonzalo "GonnSolo" García.
