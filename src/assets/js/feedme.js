(function() {

var app = angular.module("feedMe",[]);

var IndexCtrl = function($scope,$http){

  var onArticlesComplete = function(response){
    $scope.articles = response.data
    $scope.loaded = true
    };

  var onError = function(){
    $scope.error = "Could not fetch articles"
  };
  var indexArticles = function(){
    $http.get("/api/v1/articles")
         .then(onArticlesComplete, onError);
  };

  // Initialize page content
  indexArticles();
  setInterval(indexArticles,660000);



};

app.controller("IndexCtrl",IndexCtrl);

}());
