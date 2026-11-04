document.addEventListener('DOMContentLoaded', function() {
    var form = document.getElementById('contact-form');
    if (form) {
        form.addEventListener('submit', function(event) {
            event.preventDefault();
            form.reset();
            var success = document.getElementById('form-success');
            if (success) {
                success.classList.remove('hidden');
                setTimeout(() => {
                    success.classList.add('hidden');
                }, 4000);
            }
        });
    }
});
